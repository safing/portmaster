package nameserver

import (
	"context"
	"errors"
	"net"
	"strings"

	"github.com/safing/portmaster/network/packet"

	"github.com/safing/portbase/modules/subsystems"

	"github.com/safing/portbase/log"
	"github.com/safing/portbase/modules"
	"github.com/safing/portmaster/detection/dga"
	"github.com/safing/portmaster/firewall"
	"github.com/safing/portmaster/nameserver/nsutil"
	"github.com/safing/portmaster/netenv"
	"github.com/safing/portmaster/network"
	"github.com/safing/portmaster/network/netutils"
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
	module = modules.Register("nameserver", prep, start, stop, "core", "resolver")
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
		sendResponse(w, query, 0, "qclass not served", nsutil.Refused())
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
		sendResponse(w, query, 0, "invalid FQDN", nsutil.Refused())
		return nil
	}

	// start tracer
	ctx, tracer := log.AddTracer(context.Background())
	defer tracer.Submit()
	tracer.Tracef("nameserver: handling new request for %s%s from %s:%d, getting connection", q.FQDN, q.QType, remoteAddr.IP, remoteAddr.Port)

	// TODO: if there are 3 request for the same domain/type in a row, delete all caches of that domain

	// get connection
	conn := network.NewConnectionFromDNSRequest(ctx, q.FQDN, nil, packet.IPv4, remoteAddr.IP, uint16(remoteAddr.Port))

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
			tracer.Warningf("nameserver: unexpected verdict %s for connection %s, not saving", conn.Verdict, conn)
		}
	}()

	// TODO: this has been obsoleted due to special profiles
	if conn.Process().Profile() == nil {
		tracer.Infof("nameserver: failed to find process for request %s, returning NXDOMAIN", conn)
		// NOTE(ppacher): saving unknown process connection might end up in a lot of
		// processes. Consider disabling that via config.
		conn.Failed("Unknown process")
		sendResponse(w, query, conn.Verdict, conn.Reason, conn.ReasonContext)
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
		conn.Block("Possible data tunnel")
		sendResponse(w, query, conn.Verdict, conn.Reason, conn.ReasonContext)
		return nil
	}

	// check profile before we even get intel and rr
	firewall.DecideOnConnection(conn, nil)

	switch conn.Verdict {
	case network.VerdictBlock:
		tracer.Infof("nameserver: %s blocked, returning nxdomain", conn)
		sendResponse(w, query, conn.Verdict, conn.Reason, conn.ReasonContext)
		return nil
	case network.VerdictDrop, network.VerdictFailed:
		tracer.Infof("nameserver: %s dropped, not replying", conn)
		return nil
	}

	// the firewall now decided on the connection and set it to accept
	// If we have a reason context and that context implements nsutil.Responder
	// we may need to responde with something else.
	// A reason for this might be that the request is sink-holed to a forced
	// ip address in which case we "Accept" it but handle the resolving
	// differently.
	if responder, ok := conn.ReasonContext.(nsutil.Responder); ok {
		tracer.Infof("nameserver: %s handing over to reason-responder: %s", q.FQDN, conn.Reason)
		reply := responder.ReplyWithDNS(query, conn.Reason, conn.ReasonContext)
		if err := w.WriteMsg(reply); err != nil {
			tracer.Warningf("nameserver: failed to return response %s%s to %s: %s", q.FQDN, q.QType, conn.Process(), err)
		} else {
			tracer.Debugf("nameserver: returning response %s%s to %s", q.FQDN, q.QType, conn.Process())
		}

		// save dns request as open
		network.SaveOpenDNSRequest(conn)

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

		sendResponse(w, query, conn.Verdict, conn.Reason, conn.ReasonContext)
		return nil
	}

	tracer.Trace("nameserver: deciding on resolved dns")
	rrCache = firewall.DecideOnResolvedDNS(conn, q, rrCache)
	if rrCache == nil {
		sendResponse(w, query, conn.Verdict, conn.Reason, conn.ReasonContext)
		return nil
	}

	// reply to query
	m := new(dns.Msg)
	m.SetReply(query)
	m.Answer = rrCache.Answer
	m.Ns = rrCache.Ns
	m.Extra = rrCache.Extra

	if err := w.WriteMsg(m); err != nil {
		tracer.Warningf("nameserver: failed to return response %s%s to %s: %s", q.FQDN, q.QType, conn.Process(), err)
	} else {
		tracer.Debugf("nameserver: returning response %s%s to %s", q.FQDN, q.QType, conn.Process())
	}

	// save dns request as open
	network.SaveOpenDNSRequest(conn)

	return nil
}
