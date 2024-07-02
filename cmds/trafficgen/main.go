package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/miekg/dns"

	"github.com/safing/portmaster/base/log"
)

const dnsResolver = "1.1.1.1:53"

var (
	url      string
	lookup   string
	n        int
	waitMsec int
)

func init() {
	flag.StringVar(&url, "url", "", "send HTTP HEAD requests to this url")
	flag.StringVar(&lookup, "lookup", "", fmt.Sprintf("query %s for this domains", dnsResolver))
	flag.IntVar(&n, "n", 10, "how many requests to make")
	flag.IntVar(&waitMsec, "w", 100, "how many ms to wait between requests")
}

func main() {
	// Parse flags
	flag.Parse()
	if url == "" && lookup == "" {
		flag.Usage()
		os.Exit(1)
	}

	// Start logging.
	err := log.Start()
	if err != nil {
		fmt.Printf("failed to start logging: %s\n", err)
		os.Exit(1)
	}
	defer log.Shutdown()
	log.SetLogLevel(log.TraceLevel)
	log.Info("starting traffic generator")

	// Execute requests
	waitDuration := time.Duration(waitMsec) * time.Millisecond
	for i := 1; i <= n; i++ {
		makeHTTPRequest(i)
		lookupDomain(i)
		time.Sleep(waitDuration)
	}
}

func makeHTTPRequest(i int) {
	if url == "" {
		return
	}

	// Create a new client so that the connection won't be shared with other requests.
	client := http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	start := time.Now()
	resp, err := client.Head(url)
	if err != nil {
		log.Errorf("http request #%d failed after %s: %s", i, time.Since(start).Round(time.Millisecond), err)
		return
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	log.Infof("http response #%d after %s: %d", i, time.Since(start).Round(time.Millisecond), resp.StatusCode)
}

func lookupDomain(i int) {
	if lookup == "" {
		return
	}

	// Create DNS query.
	dnsQuery := new(dns.Msg)
	dnsQuery.SetQuestion(dns.Fqdn(lookup), dns.TypeA)

	// Send request.
	start := time.Now()
	reply, err := dns.Exchange(dnsQuery, dnsResolver)
	if err != nil {
		log.Errorf("dns request #%d failed after %s: %s", i, time.Since(start).Round(time.Millisecond), err)
		return
	}

	log.Infof("dns response #%d after %s: %s", i, time.Since(start).Round(time.Millisecond), dns.RcodeToString[reply.Rcode])
}
