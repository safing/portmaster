package only

import (
	"time"

	"github.com/miekg/dns"

	"github.com/Safing/portbase/log"
	"github.com/Safing/portbase/modules"

	"github.com/Safing/portmaster/analytics/algs"
	"github.com/Safing/portmaster/intel"
	"github.com/Safing/portmaster/network/netutils"
)

func init() {
	modules.Register("nameserver", nil, start, nil, "intel")
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

	// get intel and RRs
	// start = time.Now()
	_, rrCache := intel.GetIntelAndRRs(fqdn, qtype, 0)
	// log.Tracef("nameserver: took %s to get intel and RRs", time.Since(start))
	if rrCache == nil {
		// TODO: analyze nxdomain requests, malware could be trying DGA-domains
		log.Infof("nameserver: %s is nxdomain", fqdn)
		nxDomain(w, query)
		return
	}

	// reply to query
	m := new(dns.Msg)
	m.SetReply(query)
	m.Answer = rrCache.Answer
	m.Ns = rrCache.Ns
	m.Extra = rrCache.Extra
	w.WriteMsg(m)
}
