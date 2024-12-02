//go:build windows
// +build windows

package dnsmonitor

import (
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/miekg/dns"
	"github.com/safing/portmaster/service/mgr"
	"github.com/safing/portmaster/service/resolver"
)

type Listener struct {
	etw *ETWSession
}

func newListener(module *DNSMonitor) (*Listener, error) {
	// Set source of the resolver.
	ResolverInfo.Source = resolver.ServerSourceETW

	listener := &Listener{}
	// Initialize new dns event session.
	err := initializeSessions(module, listener)
	if err != nil {
		// Listen for event if the dll has been loaded
		module.instance.OSIntegration().OnInitializedEvent.AddCallback("loader-listener", func(wc *mgr.WorkerCtx, s struct{}) (cancel bool, err error) {
			err = initializeSessions(module, listener)
			if err != nil {
				return false, err
			}
			return true, nil
		})
	}
	return listener, nil
}

func initializeSessions(module *DNSMonitor, listener *Listener) error {
	var err error
	listener.etw, err = NewSession(module.instance.OSIntegration().GetETWInterface(), listener.processEvent)
	if err != nil {
		return err
	}
	// Start listener
	module.mgr.Go("etw-dns-event-listener", func(w *mgr.WorkerCtx) error {
		return listener.etw.StartTrace()
	})
	return nil
}

func (l *Listener) flush() error {
	if l.etw == nil {
		return fmt.Errorf("etw not initialized")
	}
	return l.etw.FlushTrace()
}

func (l *Listener) stop() error {
	if l == nil {
		return fmt.Errorf("listener is nil")
	}
	if l.etw == nil {
		return fmt.Errorf("invalid etw session")
	}
	// Stop and destroy trace. Destroy should be called even if stop fails for some reason.
	err := l.etw.StopTrace()
	err2 := l.etw.DestroySession()

	if err != nil {
		return fmt.Errorf("StopTrace failed: %w", err)
	}

	if err2 != nil {
		return fmt.Errorf("DestroySession failed: %w", err2)
	}
	return nil
}

func (l *Listener) processEvent(domain string, result string) {
	if processIfSelfCheckDomain(dns.Fqdn(domain)) {
		// Not need to process result.
		return
	}

	// Ignore empty results
	if len(result) == 0 {
		return
	}

	cnames := make(map[string]string)
	ips := []net.IP{}

	resultArray := strings.Split(result, ";")
	for _, r := range resultArray {
		// For results other than IP addresses, the string starts with "type:"
		if strings.HasPrefix(r, "type:") {
			dnsValueArray := strings.Split(r, " ")
			if len(dnsValueArray) < 3 {
				continue
			}

			// Ignore everything except CNAME records
			if value, err := strconv.ParseInt(dnsValueArray[1], 10, 16); err == nil && value == int64(dns.TypeCNAME) {
				cnames[domain] = dnsValueArray[2]
			}

		} else {
			// If the event doesn't start with "type:", it's an IP address
			ip := net.ParseIP(r)
			if ip != nil {
				ips = append(ips, ip)
			}
		}
	}
	saveDomain(domain, ips, cnames)
}
