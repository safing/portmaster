package resolver

import (
	"context"
	"crypto/tls"
	"net"
	"sync"
	"time"

	"github.com/miekg/dns"
	"github.com/safing/portbase/utils"
)

var (
	defaultClientTTL         = 5 * time.Minute
	defaultRequestTimeout    = 3 * time.Second // dns query
	defaultConnectTimeout    = 5 * time.Second // tcp/tls
	connectionEOLGracePeriod = 7 * time.Second

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

type dnsClientManager struct {
	lock sync.Mutex

	// set by creator
	resolver *Resolver
	ttl      time.Duration // force refresh of connection to reduce traceability
	factory  func() *dns.Client

	// internal
	pool utils.StablePool
}

type dnsClient struct {
	mgr      *dnsClientManager
	client   *dns.Client
	conn     *dns.Conn
	useUntil time.Time
}

// getConn returns the *dns.Conn and if it's new. This function may only be called between clientManager.getDNSClient() and dnsClient.done().
func (dc *dnsClient) getConn() (c *dns.Conn, new bool, err error) {
	if dc.conn == nil {
		dc.conn, err = dc.client.Dial(dc.mgr.resolver.ServerAddress)
		if err != nil {
			return nil, false, err
		}
		return dc.conn, true, nil
	}
	return dc.conn, false, nil
}

func (dc *dnsClient) addToPool() {
	dc.mgr.pool.Put(dc)
}

func (dc *dnsClient) destroy() {
	if dc.conn != nil {
		_ = dc.conn.Close()
	}
}

func newDNSClientManager(resolver *Resolver) *dnsClientManager {
	return &dnsClientManager{
		resolver: resolver,
		ttl:      0, // new client for every request, as we need to randomize the port
		factory: func() *dns.Client {
			return &dns.Client{
				Timeout: defaultRequestTimeout,
				Dialer: &net.Dialer{
					LocalAddr: getLocalAddr("udp"),
				},
			}
		},
	}
}

func newTCPClientManager(resolver *Resolver) *dnsClientManager {
	return &dnsClientManager{
		resolver: resolver,
		ttl:      defaultClientTTL,
		factory: func() *dns.Client {
			return &dns.Client{
				Net:     "tcp",
				Timeout: defaultRequestTimeout,
				Dialer: &net.Dialer{
					LocalAddr: getLocalAddr("tcp"),
					Timeout:   defaultConnectTimeout,
					KeepAlive: defaultClientTTL,
				},
			}
		},
	}
}

func newTLSClientManager(resolver *Resolver) *dnsClientManager {
	return &dnsClientManager{
		resolver: resolver,
		ttl:      defaultClientTTL,
		factory: func() *dns.Client {
			return &dns.Client{
				Net: "tcp-tls",
				TLSConfig: &tls.Config{
					MinVersion: tls.VersionTLS12,
					ServerName: resolver.VerifyDomain,
					// TODO: use portbase rng
				},
				Timeout: defaultRequestTimeout,
				Dialer: &net.Dialer{
					LocalAddr: getLocalAddr("tcp"),
					Timeout:   defaultConnectTimeout,
					KeepAlive: defaultClientTTL,
				},
			}
		},
	}
}

func (cm *dnsClientManager) getDNSClient() *dnsClient {
	cm.lock.Lock()
	defer cm.lock.Unlock()

	// return new immediately if a new client should be used for every request
	if cm.ttl == 0 {
		return &dnsClient{
			mgr:    cm,
			client: cm.factory(),
		}
	}

	// get cached client from pool
	now := time.Now().UTC()

poolLoop:
	for {
		dc, ok := cm.pool.Get().(*dnsClient)
		switch {
		case !ok || dc == nil: // cache empty (probably, pool may always return nil!)
			break poolLoop // create new
		case now.After(dc.useUntil):
			continue // get next
		default:
			return dc
		}
	}

	// no available in pool, create new
	newClient := &dnsClient{
		mgr:      cm,
		client:   cm.factory(),
		useUntil: now.Add(cm.ttl),
	}
	newClient.startCleaner()

	return newClient
}

// startCleaner waits for EOL of the client and then removes it from the pool.
func (dc *dnsClient) startCleaner() {
	// While a single worker to clean all connections may be slightly more performant, this approach focuses on least as possible locking and is simpler, thus less error prone.
	module.StartWorker("dns client cleanup", func(ctx context.Context) error {
		select {
		case <-time.After(dc.mgr.ttl + connectionEOLGracePeriod):
			// destroy
		case <-ctx.Done():
			// give a short time before kill for graceful request completion
			time.Sleep(100 * time.Millisecond)
		}
		dc.destroy()
		return nil
	})
}
