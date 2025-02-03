package dnsmonitor

import (
	"errors"
	"net"
	"strings"

	"github.com/miekg/dns"

	"github.com/safing/portmaster/base/database"
	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/service/compat"
	"github.com/safing/portmaster/service/integration"
	"github.com/safing/portmaster/service/mgr"
	"github.com/safing/portmaster/service/network/netutils"
	"github.com/safing/portmaster/service/resolver"
)

var ResolverInfo = resolver.ResolverInfo{
	Name: "SystemResolver",
	Type: resolver.ServerTypeMonitor,
}

type DNSMonitor struct {
	instance instance
	mgr      *mgr.Manager

	listener *Listener
}

// Manager returns the module manager.
func (dl *DNSMonitor) Manager() *mgr.Manager {
	return dl.mgr
}

// Start starts the module.
func (dl *DNSMonitor) Start() error {
	// Initialize dns event listener
	var err error
	dl.listener, err = newListener(dl)
	if err != nil {
		log.Warningf("dnsmonitor: failed to start dns listener: %s", err)
	}

	return nil
}

// Stop stops the module.
func (dl *DNSMonitor) Stop() error {
	if dl.listener != nil {
		err := dl.listener.stop()
		if err != nil {
			log.Errorf("dnsmonitor: failed to close listener: %s", err)
		}
	}
	return nil
}

// Flush flushes the buffer forcing all events to be processed.
func (dl *DNSMonitor) Flush() error {
	return dl.listener.flush()
}

func saveDomain(domain string, ips []net.IP, cnames map[string]string, profileScope string) {
	fqdn := dns.Fqdn(domain)
	// Create new record for this IP.
	record := resolver.ResolvedDomain{
		Domain:            fqdn,
		Resolver:          &ResolverInfo,
		DNSRequestContext: &resolver.DNSRequestContext{},
		Expires:           0,
	}

	// Process cnames
	record.AddCNAMEs(cnames)

	// Add to cache
	saveIPsInCache(ips, profileScope, record)
}

func New(instance instance) (*DNSMonitor, error) {
	// Initialize module
	m := mgr.New("DNSMonitor")
	module := &DNSMonitor{
		mgr:      m,
		instance: instance,
	}

	return module, nil
}

type instance interface {
	OSIntegration() *integration.OSIntegration
}

func processIfSelfCheckDomain(fqdn string) bool {
	// Check for compat check dns request.
	if strings.HasSuffix(fqdn, compat.DNSCheckInternalDomainScope) {
		subdomain := strings.TrimSuffix(fqdn, compat.DNSCheckInternalDomainScope)
		_ = compat.SubmitDNSCheckDomain(subdomain)
		log.Infof("dnsmonitor: self-check domain received")
		// No need to parse the answer.
		return true
	}

	return false
}

// saveIPsInCache saves the provided ips in the dns cashe assoseted with the record Domain and CNAMEs.
func saveIPsInCache(ips []net.IP, profileID string, record resolver.ResolvedDomain) {
	// Package IPs and CNAMEs into IPInfo structs.
	for _, ip := range ips {
		// Never save domain attributions for localhost IPs.
		if netutils.GetIPScope(ip) == netutils.HostLocal {
			continue
		}

		ipString := ip.String()
		info, err := resolver.GetIPInfo(profileID, ipString)
		if err != nil {
			if !errors.Is(err, database.ErrNotFound) {
				log.Errorf("dnsmonitor: failed to search for IP info record: %s", err)
			}

			info = &resolver.IPInfo{
				IP:        ipString,
				ProfileID: profileID,
			}
		}

		// Add the new record to the resolved domains for this IP and scope.
		info.AddDomain(record)

		// Save if the record is new or has been updated.
		if err := info.Save(); err != nil {
			log.Errorf("dnsmonitor: failed to save IP info record: %s", err)
		}
	}
}
