package only

import (
	"context"
	"net"
	"time"

	"github.com/miekg/dns"

	"github.com/safing/portbase/log"
	"github.com/safing/portbase/modules"

	"github.com/safing/portmaster/analytics/algs"
	"github.com/safing/portmaster/intel"
	"github.com/safing/portmaster/network/netutils"
	"github.com/safing/portmaster/status"
)

var (
	localhostIPs []dns.RR
)

func init() {
	modules.Register("nameserver", prep, start, nil, "intel")
}

func prep() error {
	intel.SetLocalAddrFactory(func(network string) net.Addr { return nil })

	localhostIPv4, err := dns.NewRR("localhost. 17 IN A 127.0.0.1")
	if err != nil {
		return err
	}

	localhostIPv6, err := dns.NewRR("localhost. 17 IN AAAA ::1")
	if err != nil {
		return err
	}

	localhostIPs = []dns.RR{localhostIPv4, localhostIPv6}

	return nil
}

func start() error {
	server := &dns.Server{Addr: "0.0.0.0:53", Net: "udp"}
	dns.HandleFunc(".", handleRequest)
	go run(server)
	return nil
}

func run(server *dns.Server) {
	for {
		err := server.ListenAndServe()
		if err != nil {
			log.Errorf("nameserver: server failed: %s", err)
			log.Info("nameserver: restarting server in 10 seconds")
			time.Sleep(10 * time.Second)
		}
	}
}

func nxDomain(w dns.ResponseWriter, query *dns.Msg) {
	m := new(dns.Msg)
	m.SetRcode(query, dns.RcodeNameError)
	w.WriteMsg(m)
}

func handleRequest(w dns.ResponseWriter, query *dns.Msg) {

	// only process first question, that's how everyone does it.
	question := query.Question[0]
	fqdn := dns.Fqdn(question.Name)
	qtype := dns.Type(question.Qtype)

	// check class
	if question.Qclass != dns.ClassINET {
		// we only serve IN records, return nxdomain
		nxDomain(w, query)
		return
	}

	// handle request for localhost
	if fqdn == "localhost." {
		m := new(dns.Msg)
		m.SetReply(query)
		m.Answer = localhostIPs
		w.WriteMsg(m)
	}

	// get addresses
	remoteAddr, ok := w.RemoteAddr().(*net.UDPAddr)
	if !ok {
		log.Warningf("nameserver: could not get remote address of request for %s%s, ignoring", fqdn, qtype)
		return
	}
	localAddr, ok := w.RemoteAddr().(*net.UDPAddr)
	if !ok {
		log.Warningf("nameserver: could not get local address of request for %s%s, ignoring", fqdn, qtype)
		return
	}

	// ignore external request
	if !remoteAddr.IP.Equal(localAddr.IP) {
		log.Warningf("nameserver: external request for %s%s, ignoring", fqdn, qtype)
		return
	}

	// check if valid domain name
	if !netutils.IsValidFqdn(fqdn) {
		log.Debugf("nameserver: domain name %s is invalid, returning nxdomain", fqdn)
		nxDomain(w, query)
		return
	}

	// start tracer
	ctx := log.AddTracer(context.Background())
	log.Tracer(ctx).Tracef("nameserver: handling new request for %s%s from %s:%d", fqdn, qtype, remoteAddr.IP, remoteAddr.Port)

	// TODO: if there are 3 request for the same domain/type in a row, delete all caches of that domain

	// check for possible DNS tunneling / data transmission
	// TODO: improve this
	lms := algs.LmsScoreOfDomain(fqdn)
	// log.Tracef("nameserver: domain %s has lms score of %f", fqdn, lms)
	if lms < 10 {
		log.WarningTracef(ctx, "nameserver: possible data tunnel by %s:%d: %s has lms score of %f, returning nxdomain", remoteAddr.IP, remoteAddr.Port, fqdn, lms)
		nxDomain(w, query)
		return
	}

	// get intel and RRs
	// start = time.Now()
	_, rrCache := intel.GetIntelAndRRs(ctx, fqdn, qtype, status.SecurityLevelDynamic)
	// log.Tracef("nameserver: took %s to get intel and RRs", time.Since(start))
	if rrCache == nil {
		// TODO: analyze nxdomain requests, malware could be trying DGA-domains
		log.WarningTracef(ctx, "nameserver: %s:%d requested %s%s, is nxdomain", remoteAddr.IP, remoteAddr.Port, fqdn, qtype)
		nxDomain(w, query)
		return
	}

	// save IP addresses to IPInfo
	for _, rr := range append(rrCache.Answer, rrCache.Extra...) {
		switch v := rr.(type) {
		case *dns.A:
			ipInfo, err := intel.GetIPInfo(v.A.String())
			if err != nil {
				ipInfo = &intel.IPInfo{
					IP:      v.A.String(),
					Domains: []string{fqdn},
				}
				ipInfo.Save()
			} else {
				if ipInfo.AddDomain(fqdn) {
					ipInfo.Save()
				}
			}
		case *dns.AAAA:
			ipInfo, err := intel.GetIPInfo(v.AAAA.String())
			if err != nil {
				ipInfo = &intel.IPInfo{
					IP:      v.AAAA.String(),
					Domains: []string{fqdn},
				}
				ipInfo.Save()
			} else {
				if ipInfo.AddDomain(fqdn) {
					ipInfo.Save()
				}
			}
		}
	}

	// reply to query
	m := new(dns.Msg)
	m.SetReply(query)
	m.Answer = rrCache.Answer
	m.Ns = rrCache.Ns
	m.Extra = rrCache.Extra
	w.WriteMsg(m)
	log.DebugTracef(ctx, "nameserver: returning response %s%s to %s:%d", fqdn, qtype, remoteAddr.IP, remoteAddr.Port)
}
