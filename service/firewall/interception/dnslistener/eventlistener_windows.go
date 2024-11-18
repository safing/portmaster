//go:build windows
// +build windows

package dnslistener

import (
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/miekg/dns"
	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/service/mgr"
	"github.com/safing/portmaster/service/network/netutils"
	"github.com/safing/portmaster/service/resolver"
)

type Listener struct {
	etw *ETWSession
}

func newListener(m *mgr.Manager) (*Listener, error) {
	listener := &Listener{}
	var err error
	listener.etw, err = NewSession("C:/Dev/ETWDNSTrace.dll", listener.processEvent)
	if err != nil {
		return nil, err
	}

	m.Go("etw-dns-event-listener", func(w *mgr.WorkerCtx) error {
		return listener.etw.StartTrace()
	})

	return listener, nil
}

func (l *Listener) flish() error {
	return l.etw.FlushTrace()
}

func (l *Listener) stop() error {
	if l == nil {
		return fmt.Errorf("listener is nil")
	}
	if l.etw == nil {
		return fmt.Errorf("invalid ewt session")
	}
	err := l.etw.StopTrace()
	err2 := l.etw.DestroySession()

	if err != nil {
		return fmt.Errorf("StopTrace failed: %d", err)
	}

	if err2 != nil {
		return fmt.Errorf("DestorySession failed: %d", err)
	}
	return nil
}

func (l *Listener) processEvent(domain string, result string) {
	if len(result) == 0 {
		return
	}

	cnames := make(map[string]string)
	ips := []net.IP{}

	resultArray := strings.Split(result, ";")
	for _, r := range resultArray {
		if strings.HasPrefix(r, "type:") {
			dnsValueArray := strings.Split(r, " ")
			if len(dnsValueArray) < 3 {
				continue
			}

			if value, err := strconv.ParseInt(dnsValueArray[1], 10, 8); err == nil && value == 5 {
				// CNAME
				cnames[domain] = dnsValueArray[2]
			}

		} else {
			ip := net.ParseIP(r)
			if ip != nil {
				ips = append(ips, ip)
			}
		}
	}

	for _, ip := range ips {
		// Never save domain attributions for localhost IPs.
		if netutils.GetIPScope(ip) == netutils.HostLocal {
			continue
		}
		fqdn := dns.Fqdn(domain)

		// Create new record for this IP.
		record := resolver.ResolvedDomain{
			Domain:            fqdn,
			Resolver:          &ResolverInfo,
			DNSRequestContext: &resolver.DNSRequestContext{},
			Expires:           0,
		}

		for {
			nextDomain, isCNAME := cnames[domain]
			if !isCNAME {
				break
			}

			record.CNAMEs = append(record.CNAMEs, nextDomain)
			domain = nextDomain
		}

		info := resolver.IPInfo{
			IP: ip.String(),
		}

		// Add the new record to the resolved domains for this IP and scope.
		info.AddDomain(record)

		// Save if the record is new or has been updated.
		if err := info.Save(); err != nil {
			log.Errorf("nameserver: failed to save IP info record: %s", err)
		}
	}
}
