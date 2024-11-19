//go:build windows
// +build windows

package dnslistener

import (
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/miekg/dns"
	"github.com/safing/portmaster/service/mgr"
)

type Listener struct {
	etw *ETWSession
}

func newListener(module *DNSListener) (*Listener, error) {
	listener := &Listener{}
	var err error
	// Intialize new dns event session.
	listener.etw, err = NewSession(module.instance.OSIntegration().GetETWInterface(), listener.processEvent)
	if err != nil {
		return nil, err
	}

	// Start lisening for events.
	module.mgr.Go("etw-dns-event-listener", func(w *mgr.WorkerCtx) error {
		return listener.etw.StartTrace()
	})

	return listener, nil
}

func (l *Listener) flush() error {
	return l.etw.FlushTrace()
}

func (l *Listener) stop() error {
	if l == nil {
		return fmt.Errorf("listener is nil")
	}
	if l.etw == nil {
		return fmt.Errorf("invalid etw session")
	}
	// Stop and destroy trace. Destory should be called even if stop failes for some reason.
	err := l.etw.StopTrace()
	err2 := l.etw.DestroySession()

	if err != nil {
		return fmt.Errorf("StopTrace failed: %d", err)
	}

	if err2 != nil {
		return fmt.Errorf("DestorySession failed: %d", err2)
	}
	return nil
}

func (l *Listener) processEvent(domain string, result string) {
	// Ignore empty results
	if len(result) == 0 {
		return
	}

	cnames := make(map[string]string)
	ips := []net.IP{}

	resultArray := strings.Split(result, ";")
	for _, r := range resultArray {
		// For result different then IP the string starts with "type:"
		if strings.HasPrefix(r, "type:") {
			dnsValueArray := strings.Split(r, " ")
			if len(dnsValueArray) < 3 {
				continue
			}

			// Ignore evrything else exept CNAME.
			if value, err := strconv.ParseInt(dnsValueArray[1], 10, 16); err == nil && value == int64(dns.TypeCNAME) {
				cnames[domain] = dnsValueArray[2]
			}

		} else {
			// The events deosn't start with "type:" that means it's an IP address.
			ip := net.ParseIP(r)
			if ip != nil {
				ips = append(ips, ip)
			}
		}
	}
	saveDomain(domain, ips, cnames)
}
