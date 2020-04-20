package nameserver

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"

	"github.com/safing/portbase/database"
	"github.com/safing/portbase/modules/subsystems"

	"github.com/safing/portbase/log"
	"github.com/safing/portbase/modules"
	"github.com/safing/portmaster/detection/dga"
	"github.com/safing/portmaster/firewall"
	"github.com/safing/portmaster/netenv"
	"github.com/safing/portmaster/network"
	"github.com/safing/portmaster/network/netutils"
	"github.com/safing/portmaster/profile/endpoints"
	"github.com/safing/portmaster/resolver"

	"github.com/miekg/dns"
)

var (
	module       *modules.Module
	dnsServer    *dns.Server
	mtDNSRequest = "dns request"

	listenAddress = "0.0.0.0:53"
	ipv4Localhost = net.IPv4(127, 0, 0, 1)
	localhostRRs  []dns.RR
)

func init() {
	module = modules.Register("nameserver", prep, start, stop, "core", "resolver", "network", "netenv")
	subsystems.Register(
		"dns",
		"Secure DNS",
		"DNS resolver with scoping and DNS-over-TLS",
		module,
		"config:dns/",
		nil,
	)
}

func prep() error {
	localhostIPv4, err := dns.NewRR("localhost. 17 IN A 127.0.0.1")
	if err != nil {
		return err
	}

	localhostIPv6, err := dns.NewRR("localhost. 17 IN AAAA ::1")
	if err != nil {
		return err
	}

	localhostRRs = []dns.RR{localhostIPv4, localhostIPv6}

	return nil
}

func start() error {
	dnsServer = &dns.Server{Addr: listenAddress, Net: "udp"}
	dns.HandleFunc(".", handleRequestAsMicroTask)

	module.StartServiceWorker("dns resolver", 0, func(ctx context.Context) error {
		err := dnsServer.ListenAndServe()
		if err != nil {
			// check if we are shutting down
			if module.IsStopping() {
				return nil
			}
			// is something blocking our port?
			checkErr := checkForConflictingService()
			if checkErr != nil {
				return checkErr
			}
		}
		return err
	})

	return nil
}

func stop() error {
	if dnsServer != nil {
		return dnsServer.Shutdown()
	}
	return nil
}

func returnNXDomain(w dns.ResponseWriter, query *dns.Msg, reason string) {
	m := new(dns.Msg)
	m.SetRcode(query, dns.RcodeNameError)
	rr, _ := dns.NewRR("portmaster.block.reason.	0	IN	TXT		" + fmt.Sprintf("%q", reason))
	m.Extra = []dns.RR{rr}
	_ = w.WriteMsg(m)
}

func returnServerFailure(w dns.ResponseWriter, query *dns.Msg) {
	m := new(dns.Msg)
	m.SetRcode(query, dns.RcodeServerFailure)
	_ = w.WriteMsg(m)
}

func handleRequestAsMicroTask(w dns.ResponseWriter, query *dns.Msg) {
	err := module.RunMicroTask(&mtDNSRequest, func(ctx context.Context) error {
		return handleRequest(ctx, w, query)
	})
	if err != nil {
		log.Warningf("intel: failed to handle dns request: %s", err)
	}
}

func handleRequest(ctx context.Context, w dns.ResponseWriter, query *dns.Msg) error { //nolint:gocognit // TODO
	// return with server failure if offline
	if netenv.GetOnlineStatus() == netenv.StatusOffline {
		returnServerFailure(w, query)
		return nil
	}

	// only process first question, that's how everyone does it.
	question := query.Question[0]
	q := &resolver.Query{
		FQDN:  question.Name,
		QType: dns.Type(question.Qtype),
	}

	// check class
	if question.Qclass != dns.ClassINET {
		// we only serve IN records, return nxdomain
		log.Warningf("nameserver: only IN record requests are supported but received Qclass %d, returning NXDOMAIN", question.Qclass)
		returnNXDomain(w, query, "wrong type")
		return nil
	}

	// handle request for localhost
	if strings.HasSuffix(q.FQDN, "localhost.") {
		m := new(dns.Msg)
		m.SetReply(query)
		m.Answer = localhostRRs
		if err := w.WriteMsg(m); err != nil {
			log.Warningf("nameserver: failed to handle request to %s: %s", q.FQDN, err)
		}
		return nil
	}

	// get addresses
	remoteAddr, ok := w.RemoteAddr().(*net.UDPAddr)
	if !ok {
		log.Warningf("nameserver: could not get remote address of request for %s%s, ignoring", q.FQDN, q.QType)
		return nil
	}
	if !remoteAddr.IP.Equal(ipv4Localhost) {
		// if request is not coming from 127.0.0.1, check if it's really local

		localAddr, ok := w.RemoteAddr().(*net.UDPAddr)
		if !ok {
			log.Warningf("nameserver: could not get local address of request for %s%s, ignoring", q.FQDN, q.QType)
			return nil
		}

		// ignore external request
		if !remoteAddr.IP.Equal(localAddr.IP) {
			log.Warningf("nameserver: external request for %s%s, ignoring", q.FQDN, q.QType)
			return nil
		}
	}

	// check if valid domain name
	if !netutils.IsValidFqdn(q.FQDN) {
		log.Debugf("nameserver: domain name %s is invalid, returning nxdomain", q.FQDN)
		returnNXDomain(w, query, "invalid domain")
		return nil
	}

	// start tracer
	ctx, tracer := log.AddTracer(ctx)
	tracer.Tracef("nameserver: handling new request for %s%s from %s:%d", q.FQDN, q.QType, remoteAddr.IP, remoteAddr.Port)

	// TODO: if there are 3 request for the same domain/type in a row, delete all caches of that domain

	// get connection
	conn := network.NewConnectionFromDNSRequest(ctx, q.FQDN, nil, remoteAddr.IP, uint16(remoteAddr.Port))

	// once we decided on the connection we might need to save it to the database
	// so we defer that check right now.
	defer func() {
		switch conn.Verdict {
		// we immediately save blocked, dropped or failed verdicts so
		// the pop up in the UI.
		case network.VerdictBlock, network.VerdictDrop, network.VerdictFailed:
			conn.Save()

		// for undecided or accepted connections we don't save them yet because
		// that will happen later anyway.
		case network.VerdictUndecided, network.VerdictAccept,
			network.VerdictRerouteToNameserver, network.VerdictRerouteToTunnel:
			return

		default:
			log.Warningf("nameserver: unexpected verdict %s for connection %s, not saving", conn.Verdict, conn)
		}
	}()

	// TODO: this has been obsoleted due to special profiles
	if conn.Process().Profile() == nil {
		tracer.Infof("nameserver: failed to find process for request %s, returning NXDOMAIN", conn)
		returnNXDomain(w, query, "unknown process")
		// NOTE(ppacher): saving unknown process connection might end up in a lot of
		// processes. Consider disabling that via config.
		conn.Failed("Unknown process")
		return nil
	}

	// save security level to query
	q.SecurityLevel = conn.Process().Profile().SecurityLevel()

	// check for possible DNS tunneling / data transmission
	// TODO: improve this
	lms := dga.LmsScoreOfDomain(q.FQDN)
	// log.Tracef("nameserver: domain %s has lms score of %f", fqdn, lms)
	if lms < 10 {
		tracer.Warningf("nameserver: possible data tunnel by %s: %s has lms score of %f, returning nxdomain", conn.Process(), q.FQDN, lms)
		returnNXDomain(w, query, "lms")
		conn.Block("Possible data tunnel")
		return nil
	}

	// check profile before we even get intel and rr
	firewall.DecideOnConnection(conn, nil)

	switch conn.Verdict {
	case network.VerdictBlock:
		tracer.Infof("nameserver: %s blocked, returning nxdomain", conn)
		returnNXDomain(w, query, conn.Reason)
		return nil
	case network.VerdictDrop, network.VerdictFailed:
		tracer.Infof("nameserver: %s dropped, not replying", conn)
		return nil
	}

	// resolve
	rrCache, err := resolver.Resolve(ctx, q)
	if err != nil {
		// TODO: analyze nxdomain requests, malware could be trying DGA-domains
		tracer.Warningf("nameserver: %s requested %s%s: %s", conn.Process(), q.FQDN, q.QType, err)

		if errors.Is(err, &resolver.BlockedUpstreamError{}) {
			conn.Block(err.Error())
		} else {
			conn.Failed("failed to resolve: " + err.Error())
		}

		returnNXDomain(w, query, conn.Reason)
		return nil
	}

	// filter DNS response
	rrCache = firewall.FilterDNSResponse(conn, q, rrCache)
	// TODO: FilterDNSResponse also sets a connection verdict
	if rrCache == nil {
		tracer.Infof("nameserver: %s implicitly denied by filtering the dns response, returning nxdomain", conn)
		returnNXDomain(w, query, conn.Reason)
		conn.Block("DNS response filtered")
		return nil
	}

	updateIPsAndCNAMEs(q, rrCache, conn)

	// if we have CNAMEs and the profile is configured to filter them
	// we need to re-check the lists and endpoints here
	if conn.Process().Profile().FilterCNAMEs() {
		conn.Entity.ResetLists()
		conn.Entity.EnableCNAMECheck(true)

		result, reason := conn.Process().Profile().MatchEndpoint(conn.Entity)
		if result == endpoints.Denied {
			conn.Block("endpoint in blocklist: " + reason)
			returnNXDomain(w, query, conn.Reason)
			return nil
		}

		if result == endpoints.NoMatch {
			result, reason = conn.Process().Profile().MatchFilterLists(conn.Entity)
			if result == endpoints.Denied {
				conn.Block("endpoint in filterlists: " + reason)
				returnNXDomain(w, query, conn.Reason)
				return nil
			}
		}
	}

	// reply to query
	m := new(dns.Msg)
	m.SetReply(query)
	m.Answer = rrCache.Answer
	m.Ns = rrCache.Ns
	m.Extra = rrCache.Extra

	if err := w.WriteMsg(m); err != nil {
		log.Warningf("nameserver: failed to return response %s%s to %s: %s", q.FQDN, q.QType, conn.Process(), err)
	} else {
		tracer.Debugf("nameserver: returning response %s%s to %s", q.FQDN, q.QType, conn.Process())
	}

	// save dns request as open
	network.SaveOpenDNSRequest(conn)

	return nil
}

func updateIPsAndCNAMEs(q *resolver.Query, rrCache *resolver.RRCache, conn *network.Connection) {
	// save IP addresses to IPInfo
	cnames := make(map[string]string)
	ips := make(map[string]struct{})

	for _, rr := range append(rrCache.Answer, rrCache.Extra...) {
		switch v := rr.(type) {
		case *dns.CNAME:
			cnames[v.Hdr.Name] = v.Target

		case *dns.A:
			ips[v.A.String()] = struct{}{}

		case *dns.AAAA:
			ips[v.AAAA.String()] = struct{}{}
		}
	}

	for ip := range ips {
		record := resolver.ResolvedDomain{
			Domain: q.FQDN,
		}

		// resolve all CNAMEs in the correct order.
		var domain = q.FQDN
		for {
			nextDomain, isCNAME := cnames[domain]
			if !isCNAME {
				break
			}

			record.CNAMEs = append(record.CNAMEs, nextDomain)
			domain = nextDomain
		}

		// update the entity to include the cnames
		conn.Entity.CNAME = record.CNAMEs

		// get the existing IP info or create a new  one
		var save bool
		info, err := resolver.GetIPInfo(ip)
		if err != nil {
			if err != database.ErrNotFound {
				log.Errorf("nameserver: failed to search for IP info record: %s", err)
			}

			info = &resolver.IPInfo{
				IP: ip,
			}
			save = true
		}

		// and the new resolved domain record and save
		if new := info.AddDomain(record); new {
			save = true
		}
		if save {
			if err := info.Save(); err != nil {
				log.Errorf("nameserver: failed to save IP info record: %s", err)
			}
		}
	}
}
