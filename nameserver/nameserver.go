// Copyright Safing ICS Technologies GmbH. Use of this source code is governed by the AGPL license that can be found in the LICENSE file.

package nameserver

import (
	"net"
	"time"

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

func init() {
	modules.Register("nameserver", prep, start, nil, "intel")
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
	server := &dns.Server{Addr: "127.0.0.1:53", Net: "udp"}
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

	// TODO: if there are 3 request for the same domain/type in a row, delete all caches of that domain

	// only process first question, that's how everyone does it.
	question := query.Question[0]
	fqdn := dns.Fqdn(question.Name)
	qtype := dns.Type(question.Qtype)

	// use this to time how long it takes process this request
	// timed := time.Now()
	// defer log.Tracef("nameserver: took %s to handle request for %s%s", time.Now().Sub(timed).String(), fqdn, qtype.String())

	// check if valid domain name
	if !netutils.IsValidFqdn(fqdn) {
		log.Tracef("nameserver: domain name %s is invalid, returning nxdomain", fqdn)
		nxDomain(w, query)
		return
	}

	// check for possible DNS tunneling / data transmission
	// TODO: improve this
	lms := algs.LmsScoreOfDomain(fqdn)
	// log.Tracef("nameserver: domain %s has lms score of %f", fqdn, lms)
	if lms < 10 {
		log.Tracef("nameserver: possible data tunnel: %s has lms score of %f, returning nxdomain", fqdn, lms)
		nxDomain(w, query)
		return
	}

	// check class
	if question.Qclass != dns.ClassINET {
		// we only serve IN records, send NXDOMAIN
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

	// get remote address
	// start := time.Now()
	rAddr, ok := w.RemoteAddr().(*net.UDPAddr)
	// log.Tracef("nameserver: took %s to get remote address", time.Since(start))
	if !ok {
		log.Warningf("nameserver: could not get address of request, returning nxdomain")
		nxDomain(w, query)
		return
	}

	// [1/2] use this to time how long it takes to get process info
	// timed := time.Now()

	// get connection
	// start = time.Now()
	comm, err := network.GetCommunicationByDNSRequest(rAddr.IP, uint16(rAddr.Port), fqdn)
	// log.Tracef("nameserver: took %s to get comms (and maybe process)", time.Since(start))
	if err != nil {
		log.Warningf("nameserver: someone is requesting %s, but could not identify process: %s, returning nxdomain", fqdn, err)
		nxDomain(w, query)
		return
	}

	// [2/2] use this to time how long it takes to get process info
	// log.Tracef("nameserver: took %s to get connection/process of %s request", time.Now().Sub(timed).String(), fqdn)

	// check profile before we even get intel and rr
	// start = time.Now()
	firewall.DecideOnCommunicationBeforeIntel(comm, fqdn)
	// log.Tracef("nameserver: took %s to make decision", time.Since(start))

	if comm.GetVerdict() == network.VerdictBlock || comm.GetVerdict() == network.VerdictDrop {
		nxDomain(w, query)
		return
	}

	// get intel and RRs
	// start = time.Now()
	domainIntel, rrCache := intel.GetIntelAndRRs(fqdn, qtype, comm.Process().ProfileSet().SecurityLevel())
	// log.Tracef("nameserver: took %s to get intel and RRs", time.Since(start))
	if rrCache == nil {
		// TODO: analyze nxdomain requests, malware could be trying DGA-domains
		log.Infof("nameserver: %s tried to query %s, but is nxdomain", comm.Process().String(), fqdn)
		nxDomain(w, query)
		return
	}

	// set intel
	comm.Lock()
	comm.Intel = domainIntel
	comm.Unlock()
	comm.Save()

	// check with intel
	firewall.DecideOnCommunicationAfterIntel(comm, fqdn, rrCache)
	switch comm.GetVerdict() {
	case network.VerdictUndecided, network.VerdictBlock, network.VerdictDrop:
		nxDomain(w, query)
		return
	}

	// filter DNS response
	rrCache = firewall.FilterDNSResponse(comm, fqdn, rrCache)
	if rrCache == nil {
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
}
