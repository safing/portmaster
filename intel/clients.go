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

// SetLocalAddrFactory supplies the intel package with a function to get permitted local addresses for connections.
func SetLocalAddrFactory(laf func(network string) net.Addr) {
	if localAddrFactory == nil {
		localAddrFactory = laf
	}
}

func getLocalAddr(network string) net.Addr {
	if localAddrFactory != nil {
		return localAddrFactory(network)
	}
	return nil
}

type clientManager struct {
	dnsClient *dns.Client
	factory   func() *dns.Client

	lock         sync.Mutex
	refreshAfter time.Time
	ttl          time.Duration // force refresh of connection to reduce traceability
}

func newDNSClientManager(_ *Resolver) *clientManager {
	return &clientManager{
		ttl: 0, // new client for every request, as we need to randomize the port
		factory: func() *dns.Client {
			return &dns.Client{
				Timeout: 5 * time.Second,
				Dialer: &net.Dialer{
					LocalAddr: getLocalAddr("udp"),
				},
			}
		},
	}
}

func newTCPClientManager(_ *Resolver) *clientManager {
	return &clientManager{
		ttl: 0, // TODO: build a custom client that can reuse connections to some degree (performance / privacy tradeoff)
		factory: func() *dns.Client {
			return &dns.Client{
				Net:     "tcp",
				Timeout: 5 * time.Second,
				Dialer: &net.Dialer{
					LocalAddr: getLocalAddr("tcp"),
					KeepAlive: 15 * time.Second,
				},
			}
		},
	}
}

func newTLSClientManager(resolver *Resolver) *clientManager {
	return &clientManager{
		ttl: 0, // TODO: build a custom client that can reuse connections to some degree (performance / privacy tradeoff)
		factory: func() *dns.Client {
			return &dns.Client{
				Net: "tcp-tls",
				TLSConfig: &tls.Config{
					MinVersion: tls.VersionTLS12,
					ServerName: resolver.VerifyDomain,
					// TODO: use portbase rng
				},
				Timeout: 5 * time.Second,
				Dialer: &net.Dialer{
					LocalAddr: getLocalAddr("tcp"),
					KeepAlive: 15 * time.Second,
				},
			}
		},
	}
}

func newHTTPSClientManager(resolver *Resolver) *clientManager {
	return &clientManager{
		ttl: 0, // TODO: build a custom client that can reuse connections to some degree (performance / privacy tradeoff)
		factory: func() *dns.Client {
			new := &dns.Client{
				Net: "https",
				TLSConfig: &tls.Config{
					MinVersion: tls.VersionTLS12,
					// TODO: use portbase rng
				},
				Timeout: 5 * time.Second,
				Dialer: &net.Dialer{
					LocalAddr: getLocalAddr("tcp"),
					KeepAlive: 15 * time.Second,
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
