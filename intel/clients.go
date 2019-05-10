package intel

import (
	"crypto/tls"
	"net"
	"sync"
	"time"

	"github.com/miekg/dns"
)

var (
	localAddrFactory func(network string) net.Addr
)

// SetLocalAddrFactory supplied the intel package with a function to set local addresses for connections.
func SetLocalAddrFactory(laf func(network string) net.Addr) {
	if localAddrFactory == nil {
		localAddrFactory = laf
	}
}

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
		// ttl: 1 * time.Minute,
		factory: func() *dns.Client {
			return &dns.Client{
				Timeout: 5 * time.Second,
				Dialer: &net.Dialer{
					LocalAddr: localAddrFactory("udp"),
				},
			}
		},
	}
}

func newTCPClientManager(resolver *Resolver) *clientManager {
	return &clientManager{
		// ttl: 5 * time.Minute,
		factory: func() *dns.Client {
			return &dns.Client{
				Net:     "tcp",
				Timeout: 5 * time.Second,
				Dialer: &net.Dialer{
					LocalAddr: localAddrFactory("tcp"),
				},
			}
		},
	}
}

func newTLSClientManager(resolver *Resolver) *clientManager {
	return &clientManager{
		// ttl: 5 * time.Minute,
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
				Dialer: &net.Dialer{
					LocalAddr: localAddrFactory("tcp"),
				},
			}
		},
	}
}

func newHTTPSClientManager(resolver *Resolver) *clientManager {
	return &clientManager{
		// ttl: 5 * time.Minute,
		factory: func() *dns.Client {
			new := &dns.Client{
				Net: "https",
				TLSConfig: &tls.Config{
					MinVersion: tls.VersionTLS12,
					// TODO: use custom random
					// Rand: io.Reader,
				},
				Timeout: 5 * time.Second,
				Dialer: &net.Dialer{
					LocalAddr: localAddrFactory("tcp"),
				},
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

	if cm.dnsClient == nil || cm.ttl == 0 || time.Now().After(cm.refreshAfter) {
		cm.dnsClient = cm.factory()
		cm.refreshAfter = time.Now().Add(cm.ttl)
	}

	return cm.dnsClient
}
