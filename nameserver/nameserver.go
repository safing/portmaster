// Copyright Safing ICS Technologies GmbH. Use of this source code is governed by the AGPL license that can be found in the LICENSE file.

package nameserver

import (
	"net"

	"github.com/miekg/dns"

	"github.com/Safing/safing-core/analytics/algs"
	"github.com/Safing/safing-core/intel"
	"github.com/Safing/safing-core/log"
	"github.com/Safing/safing-core/modules"
	"github.com/Safing/safing-core/network"
	"github.com/Safing/safing-core/network/netutils"
	"github.com/Safing/safing-core/portmaster"
)

var (
	nameserverModule *modules.Module
)

func init() {
	nameserverModule = modules.Register("Nameserver", 128)
}

func Start() {
	server := &dns.Server{Addr: "127.0.0.1:53", Net: "udp"}
	dns.HandleFunc(".", handleRequest)
	go func() {
		err := server.ListenAndServe()
		if err != nil {
			log.Errorf("nameserver: server failed: %s", err)
		}
	}()
	// TODO: stop mocking
	defer nameserverModule.StopComplete()
	<-nameserverModule.Stop
}

func nxDomain(w dns.ResponseWriter, query *dns.Msg) {
	m := new(dns.Msg)
	m.SetRcode(query, dns.RcodeNameError)
	w.WriteMsg(m)
}

func handleRequest(w dns.ResponseWriter, query *dns.Msg) {

	// TODO: if there are 3 request for the same domain/type in a row, delete all caches of that domain
	// TODO: handle securityLevelOff

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
	connection, err := network.GetConnectionByDNSRequest(rAddr.IP, uint16(rAddr.Port), fqdn)
	// log.Tracef("nameserver: took %s to get connection (and maybe process)", time.Since(start))
	if err != nil {
		log.Warningf("nameserver: someone is requesting %s, but could not identify process: %s, returning nxdomain", fqdn, err)
		nxDomain(w, query)
		return
	}

	// [2/2] use this to time how long it takes to get process info
	// log.Tracef("nameserver: took %s to get connection/process of %s request", time.Now().Sub(timed).String(), fqdn)

	// check profile before we even get intel and rr
	if connection.Verdict == network.UNDECIDED {
		// start = time.Now()
		portmaster.DecideOnConnectionBeforeIntel(connection, fqdn)
		// log.Tracef("nameserver: took %s to make decision", time.Since(start))
	}
	if connection.Verdict == network.BLOCK || connection.Verdict == network.DROP {
		nxDomain(w, query)
		return
	}

	// get intel and RRs
	// start = time.Now()
	domainIntel, rrCache := intel.GetIntelAndRRs(fqdn, qtype, connection.Process().Profile.SecurityLevel)
	// log.Tracef("nameserver: took %s to get intel and RRs", time.Since(start))
	if rrCache == nil {
		// TODO: analyze nxdomain requests, malware could be trying DGA-domains
		log.Infof("nameserver: %s tried to query %s, but is nxdomain", connection.Process().String(), fqdn)
		nxDomain(w, query)
		return
	}

	// set intel
	connection.Intel = domainIntel
	connection.Save()

	// do a full check with intel
	if connection.Verdict == network.UNDECIDED {
		rrCache = portmaster.DecideOnConnectionAfterIntel(connection, fqdn, rrCache)
	}
	if rrCache == nil || connection.Verdict == network.BLOCK || connection.Verdict == network.DROP {
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
					Domains: []string{fqdn},
				}
				ipInfo.Create(v.A.String())
			} else {
				ipInfo.Domains = append(ipInfo.Domains, fqdn)
				ipInfo.Save()
			}
		case *dns.AAAA:
			ipInfo, err := intel.GetIPInfo(v.AAAA.String())
			if err != nil {
				ipInfo = &intel.IPInfo{
					Domains: []string{fqdn},
				}
				ipInfo.Create(v.AAAA.String())
			} else {
				ipInfo.Domains = append(ipInfo.Domains, fqdn)
				ipInfo.Save()
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
