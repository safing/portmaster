package intel

import (
	"crypto/tls"
	"sync"
	"time"

	"github.com/miekg/dns"
)

type clientManager struct {
	dnsClient *dns.Client
	factory   func() *dns.Client

	lock         sync.Mutex
	refreshAfter time.Time
	ttl          time.Duration // force refresh of connection to reduce traceability
}

// ref: https://godoc.org/github.com/miekg/dns#Client

func newDNSClientManager(resolver *Resolver) *clientManager {
	return &clientManager{
		ttl: -1 * time.Minute,
		factory: func() *dns.Client {
			return &dns.Client{
				Timeout: 5 * time.Second,
			}
		},
	}
}

func newTCPClientManager(resolver *Resolver) *clientManager {
	return &clientManager{
		ttl: -15 * time.Minute,
		factory: func() *dns.Client {
			return &dns.Client{
				Net:     "tcp",
				Timeout: 5 * time.Second,
			}
		},
	}
}

func newTLSClientManager(resolver *Resolver) *clientManager {
	return &clientManager{
		ttl: -15 * time.Minute,
		factory: func() *dns.Client {
			return &dns.Client{
				Net: "tcp-tls",
				TLSConfig: &tls.Config{
					MinVersion: tls.VersionTLS12,
					ServerName: resolver.VerifyDomain,
					// TODO: use custom random
					// Rand: io.Reader,
				},
				Timeout: 5 * time.Second,
			}
		},
	}
}

func newHTTPSClientManager(resolver *Resolver) *clientManager {
	return &clientManager{
		ttl: -15 * time.Minute,
		factory: func() *dns.Client {
			new := &dns.Client{
				Net: "https",
				TLSConfig: &tls.Config{
					MinVersion: tls.VersionTLS12,
					// TODO: use custom random
					// Rand: io.Reader,
				},
				Timeout: 5 * time.Second,
			}
			if resolver.VerifyDomain != "" {
				new.TLSConfig.ServerName = resolver.VerifyDomain
			}
			return new
		},
	}
}

func (cm *clientManager) getDNSClient() *dns.Client {
	cm.lock.Lock()
	defer cm.lock.Unlock()

	if cm.dnsClient == nil || time.Now().After(cm.refreshAfter) {
		cm.dnsClient = cm.factory()
		cm.refreshAfter = time.Now().Add(cm.ttl)
	}

	return cm.dnsClient
}
