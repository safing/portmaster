// Copyright Safing ICS Technologies GmbH. Use of this source code is governed by the AGPL license that can be found in the LICENSE file.

package nameserver

import (
	"context"
	"net"
	"runtime"

	"github.com/miekg/dns"

	"github.com/Safing/portbase/log"
	"github.com/Safing/portbase/modules"

	"github.com/Safing/portmaster/analytics/algs"
	"github.com/Safing/portmaster/firewall"
	"github.com/Safing/portmaster/intel"
	"github.com/Safing/portmaster/network"
	"github.com/Safing/portmaster/network/netutils"
)

var (
	localhostIPs []dns.RR
)

var (
	listenAddress = "127.0.0.1:53"
	localhostIP   = net.IPv4(127, 0, 0, 1)
)

func init() {
	modules.Register("nameserver", prep, start, nil, "intel")

	if runtime.GOOS == "windows" {
		listenAddress = "0.0.0.0:53"
	}
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

	localhostIPs = []dns.RR{localhostIPv4, localhostIPv6}

	return nil
}

func start() error {
	server := &dns.Server{Addr: listenAddress, Net: "udp"}
	dns.HandleFunc(".", handleRequest)
	go run(server)
	return nil
}

func run(server *dns.Server) {
	for {
		err := server.ListenAndServe()
		if err != nil {
			log.Errorf("nameserver: server failed: %s", err)
			checkForConflictingService(err)
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
	if !remoteAddr.IP.Equal(localhostIP) {
		// if request is not coming from 127.0.0.1, check if it's really local

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

	// get connection
	comm, err := network.GetCommunicationByDNSRequest(ctx, remoteAddr.IP, uint16(remoteAddr.Port), fqdn)
	if err != nil {
		log.ErrorTracef(ctx, "nameserver: could not identify process of %s:%d, returning nxdomain: %s", remoteAddr.IP, remoteAddr.Port, err)
		nxDomain(w, query)
		return
	}
	defer func() {
		go comm.SaveIfNeeded()
	}()

	// check for possible DNS tunneling / data transmission
	// TODO: improve this
	lms := algs.LmsScoreOfDomain(fqdn)
	// log.Tracef("nameserver: domain %s has lms score of %f", fqdn, lms)
	if lms < 10 {
		log.WarningTracef(ctx, "nameserver: possible data tunnel by %s: %s has lms score of %f, returning nxdomain", comm.Process(), fqdn, lms)
		nxDomain(w, query)
		return
	}

	// check profile before we even get intel and rr
	firewall.DecideOnCommunicationBeforeIntel(comm, fqdn)
	comm.Lock()
	comm.SaveWhenFinished()
	comm.Unlock()

	if comm.GetVerdict() == network.VerdictBlock || comm.GetVerdict() == network.VerdictDrop {
		log.InfoTracef(ctx, "nameserver: %s denied before intel, returning nxdomain", comm)
		nxDomain(w, query)
		return
	}

	// get intel and RRs
	domainIntel, rrCache := intel.GetIntelAndRRs(ctx, fqdn, qtype, comm.Process().ProfileSet().SecurityLevel())
	if rrCache == nil {
		// TODO: analyze nxdomain requests, malware could be trying DGA-domains
		log.WarningTracef(ctx, "nameserver: %s requested %s%s, is nxdomain", comm.Process(), fqdn, qtype)
		nxDomain(w, query)
		return
	}

	// set intel
	comm.Lock()
	comm.Intel = domainIntel
	comm.Unlock()

	// check with intel
	firewall.DecideOnCommunicationAfterIntel(comm, fqdn, rrCache)
	switch comm.GetVerdict() {
	case network.VerdictUndecided, network.VerdictBlock, network.VerdictDrop:
		log.InfoTracef(ctx, "nameserver: %s denied after intel, returning nxdomain", comm)
		nxDomain(w, query)
		return
	}

	// filter DNS response
	rrCache = firewall.FilterDNSResponse(comm, fqdn, rrCache)
	if rrCache == nil {
		log.InfoTracef(ctx, "nameserver: %s implicitly denied by filtering the dns response, returning nxdomain", comm)
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
	log.DebugTracef(ctx, "nameserver: returning response %s%s to %s", fqdn, qtype, comm.Process())
}
