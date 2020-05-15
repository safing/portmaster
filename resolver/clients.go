package resolver

import (
	"context"
	"crypto/tls"
	"net"
	"sync"
	"time"

	"github.com/miekg/dns"
)

const (
	defaultClientTTL         = 5 * time.Minute
	defaultRequestTimeout    = 5 * time.Second
	connectionEOLGracePeriod = 10 * time.Second
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

type dnsClientManager struct {
	lock sync.Mutex

	// set by creator
	serverAddress string
	ttl           time.Duration // force refresh of connection to reduce traceability
	factory       func() *dns.Client

	// internal
	pool []*dnsClient
}

type dnsClient struct {
	mgr *dnsClientManager

	inUse     bool
	useUntil  time.Time
	dead      bool
	inPool    bool
	poolIndex int

	client *dns.Client
	conn   *dns.Conn
}

// conn returns the *dns.Conn and if it's new. This function may only be called between clientManager.getDNSClient() and dnsClient.done().
func (dc *dnsClient) getConn() (c *dns.Conn, new bool, err error) {
	if dc.conn == nil {
		dc.conn, err = dc.client.Dial(dc.mgr.serverAddress)
		if err != nil {
			return nil, false, err
		}
		return dc.conn, true, nil
	}
	return dc.conn, false, nil
}

func (dc *dnsClient) done() {
	dc.mgr.lock.Lock()
	defer dc.mgr.lock.Unlock()

	dc.inUse = false
}

func (dc *dnsClient) destroy() {
	dc.mgr.lock.Lock()
	dc.inUse = true // block from being used
	dc.dead = true  // abort cleaning
	if dc.inPool {
		dc.inPool = false
		dc.mgr.pool[dc.poolIndex] = nil
	}
	dc.mgr.lock.Unlock()

	if dc.conn != nil {
		_ = dc.conn.Close()
	}
}

func newDNSClientManager(resolver *Resolver) *dnsClientManager {
	return &dnsClientManager{
		serverAddress: resolver.ServerAddress,
		ttl:           0, // new client for every request, as we need to randomize the port
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
		serverAddress: resolver.ServerAddress,
		ttl:           defaultClientTTL,
		factory: func() *dns.Client {
			return &dns.Client{
				Net:     "tcp",
				Timeout: defaultRequestTimeout,
				Dialer: &net.Dialer{
					LocalAddr: getLocalAddr("tcp"),
					KeepAlive: defaultClientTTL,
				},
			}
		},
	}
}

func newTLSClientManager(resolver *Resolver) *dnsClientManager {
	return &dnsClientManager{
		serverAddress: resolver.ServerAddress,
		ttl:           defaultClientTTL,
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

	// get first unused from pool
	now := time.Now().UTC()
	for _, dc := range cm.pool {
		if dc != nil && !dc.inUse && now.Before(dc.useUntil) {
			dc.inUse = true
			return dc
		}
	}

	// no available in pool, create new
	newClient := &dnsClient{
		mgr:      cm,
		inUse:    true,
		useUntil: now.Add(cm.ttl),
		inPool:   true,
		client:   cm.factory(),
	}
	newClient.startCleaner()

	// find free spot in pool
	for poolIndex, dc := range cm.pool {
		if dc == nil {
			cm.pool[poolIndex] = newClient
			newClient.poolIndex = poolIndex
			return newClient
		}
	}

	// append to pool
	cm.pool = append(cm.pool, newClient)
	newClient.poolIndex = len(cm.pool) - 1
	// TODO: shrink pool again?

	return newClient
}

// startCleaner waits for EOL of the client and then removes it from the pool.
func (dc *dnsClient) startCleaner() {
	// While a single worker to clean all connections may be slightly more performant, this approach focuses on least as possible locking and is simpler, thus less error prone.
	module.StartWorker("dns client cleanup", func(ctx context.Context) error {
		select {
		case <-time.After(dc.mgr.ttl + time.Second):
			dc.mgr.lock.Lock()
			cleanNow := dc.dead || !dc.inUse
			dc.mgr.lock.Unlock()

			if cleanNow {
				dc.destroy()
				return nil
			}
		case <-ctx.Done():
			// give a short time before kill for graceful request completion
			time.Sleep(100 * time.Millisecond)
		}

		// wait for grace period to end, then kill
		select {
		case <-time.After(connectionEOLGracePeriod):
		case <-ctx.Done():
		}

		dc.destroy()
		return nil
	})
}
