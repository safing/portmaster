package dnslistener

import (
	"errors"
	"net"
	"sync/atomic"

	"github.com/miekg/dns"
	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/service/integration"
	"github.com/safing/portmaster/service/mgr"
	"github.com/safing/portmaster/service/network/netutils"
	"github.com/safing/portmaster/service/resolver"
)

var ResolverInfo = resolver.ResolverInfo{
	Name:   "SystemdResolver",
	Type:   "env",
	Source: "System",
}

type DNSListener struct {
	instance instance
	mgr      *mgr.Manager

	listener *Listener
}

// Manager returns the module manager.
func (dl *DNSListener) Manager() *mgr.Manager {
	return dl.mgr
}

// Start starts the module.
func (dl *DNSListener) Start() error {
	// Initialize dns event listener
	var err error
	dl.listener, err = newListener(dl)
	if err != nil {
		log.Errorf("failed to start dns listener: %s", err)
	}

	return nil
}

// Stop stops the module.
func (dl *DNSListener) Stop() error {
	if dl.listener != nil {
		err := dl.listener.stop()
		if err != nil {
			log.Errorf("failed to close listener: %s", err)
		}
	}
	return nil
}

// Flush flushes the buffer forcing all events to be processed.
func (dl *DNSListener) Flush() error {
	return dl.listener.flush()
}

func saveDomain(domain string, ips []net.IP, cnames map[string]string) {
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

var shimLoaded atomic.Bool

func New(instance instance) (*DNSListener, error) {
	if !shimLoaded.CompareAndSwap(false, true) {
		return nil, errors.New("only one instance allowed")
	}
	// Initialize module
	m := mgr.New("DNSListener")
	module := &DNSListener{
		mgr:      m,
		instance: instance,
	}
	return module, nil
}

type instance interface {
	OSIntegration() *integration.OSIntegration
}
