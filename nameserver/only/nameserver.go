package only

import (
	"context"
	"net"
	"strings"

	"github.com/safing/portbase/database"
	"github.com/safing/portbase/log"
	"github.com/safing/portbase/modules"
	"github.com/safing/portmaster/netenv"
	"github.com/safing/portmaster/network/netutils"
	"github.com/safing/portmaster/resolver"

	"github.com/miekg/dns"
)

var (
	module       *modules.Module
	dnsServer    *dns.Server
	mtDNSRequest = "dns request"

	listenAddress = "127.0.0.1:53"
	ipv4Localhost = net.IPv4(127, 0, 0, 1)
	localhostRRs  []dns.RR
)

func init() {
	module = modules.Register("nameserver", initLocalhostRRs, start, stop, "core", "resolver", "network", "netenv")
}

func initLocalhostRRs() error {
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

func returnNXDomain(w dns.ResponseWriter, query *dns.Msg) {
	m := new(dns.Msg)
	m.SetRcode(query, dns.RcodeNameError)
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
		log.Warningf("nameserver: failed to handle dns request: %s", err)
	}
}

func handleRequest(ctx context.Context, w dns.ResponseWriter, query *dns.Msg) error {
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
		returnNXDomain(w, query)
		return nil
	}

	// handle request for localhost
	if strings.HasSuffix(q.FQDN, "localhost.") {
		m := new(dns.Msg)
		m.SetReply(query)
		m.Answer = localhostRRs
		_ = w.WriteMsg(m)
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
		returnNXDomain(w, query)
		return nil
	}

	// start tracer
	ctx, tracer := log.AddTracer(ctx)
	tracer.Tracef("nameserver: handling new request for %s%s from %s:%d", q.FQDN, q.QType, remoteAddr.IP, remoteAddr.Port)

	// TODO: if there are 3 request for the same domain/type in a row, delete all caches of that domain

	// get intel and RRs
	rrCache, err := resolver.Resolve(ctx, q)
	if err != nil {
		// TODO: analyze nxdomain requests, malware could be trying DGA-domains
		tracer.Warningf("nameserver: request for %s%s: %s", q.FQDN, q.QType, err)
		returnNXDomain(w, query)
		return nil
	}

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

	// reply to query
	m := new(dns.Msg)
	m.SetReply(query)
	m.Answer = rrCache.Answer
	m.Ns = rrCache.Ns
	m.Extra = rrCache.Extra
	_ = w.WriteMsg(m)
	tracer.Debugf("nameserver: returning response %s%s", q.FQDN, q.QType)

	return nil
}
