package splittun

import (
	"fmt"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/safing/portmaster/service/mgr"
	"github.com/safing/portmaster/service/netenv"
	"github.com/safing/portmaster/service/network"
	"github.com/safing/portmaster/service/network/packet"
	"github.com/safing/portmaster/service/splittun/proxy"
)

// pendingRequestTTL is the maximum time a pending request waits for the proxy
// to accept the redirected connection. If the OS drops/resets the connection
// before it reaches the proxy, the entry would otherwise leak indefinitely.
const pendingRequestTTL = 30 * time.Second

type request struct {
	connInfo  *network.Connection
	binding   proxy.LocalBinding
	expiresAt time.Time
}

var (
	requestsLock     sync.Mutex
	pendingRequests  map[string]*request = make(map[string]*request) // key: "localIP:localPort"
	cleanupScheduled atomic.Bool
)

// AwaitRequest registers a connection for handling when it arrives at the proxy.
// The bindInterface must be unique info which identifies the interface to bind to:
// - interface local IP address (e.g. "192.168.1.1")
// - interface name (e.g. "eth0")
// - MAC address (e.g. "00:1A:2B:3C:4D:5E")
// - empty - to try detecting "default" (non-VPN) interface automatically (not reliable)
func AwaitRequest(connInfo *network.Connection, bindInterface string) (*network.SplitTunContext, error) {

	var binding proxy.LocalBinding
	if bindInterface == "" {
		// empty - is the default and means to try detecting the "default" (non-VPN) interface automatically.
		// This is not reliable, but can be convenient for users who don't want to configure an interface.
		ifaces, err := netenv.GetBestPhysicalDefaultInterfaces()
		if err != nil {
			return nil, err
		}

		var selectedIface *netenv.InterfaceInfo
		if connInfo.IPVersion == packet.IPv6 && ifaces.ForIPv6 != nil {
			selectedIface = ifaces.ForIPv6
			binding.IP = selectedIface.IPv6
		} else if connInfo.IPVersion == packet.IPv4 && ifaces.ForIPv4 != nil {
			selectedIface = ifaces.ForIPv4
			binding.IP = selectedIface.IPv4
		} else {
			return nil, fmt.Errorf("no suitable default physical interface found for %s", connInfo.IPVersion)
		}
		binding.Interface = selectedIface.Interface.Name
	} else {
		// Getting the interface IP address to bind the proxy connection to.
		iface, err := netenv.GetInterface(bindInterface)
		if err != nil {
			return nil, err
		}

		if connInfo.IPVersion == packet.IPv6 {
			binding.IP = iface.IPv6
		} else {
			binding.IP = iface.IPv4
		}
		if binding.IP == nil {
			return nil, fmt.Errorf("interface %q has no usable address for %s", bindInterface, connInfo.IPVersion)
		}
		binding.Interface = iface.Interface.Name
	}

	// Create unique key for the pending connection
	if connInfo.LocalIP == nil {
		return nil, fmt.Errorf("connection has no local IP")
	}
	key := net.JoinHostPort(connInfo.LocalIP.String(), strconv.Itoa(int(connInfo.LocalPort)))

	requestsLock.Lock()
	defer requestsLock.Unlock()

	// Register the request
	if _, exists := pendingRequests[key]; exists {
		return nil, fmt.Errorf("a pending request for %s already exists", key)
	}

	pendingRequests[key] = &request{
		connInfo:  connInfo,
		binding:   binding,
		expiresAt: time.Now().Add(pendingRequestTTL),
	}

	// Schedule deferred cleanup outside of the hot path.
	// The goroutine only starts if none is already running.
	scheduleCleanup()

	return &network.SplitTunContext{
		Interface: binding.Interface,
		IP:        binding.IP,
	}, nil
}

// scheduleCleanup starts a deferred cleanup goroutine if one is not already
// running. The goroutine wakes after pendingRequestTTL+1s, sweeps expired
// entries, and reschedules itself if unexpired entries remain. It exits
// immediately when the module's manager context is cancelled.
func scheduleCleanup() {
	if !cleanupScheduled.CompareAndSwap(false, true) {
		return // already scheduled; it will sweep our entry too
	}
	module.mgr.Go("pending-requests-cleanup", func(w *mgr.WorkerCtx) error {
		select {
		case <-w.Done():
			cleanupScheduled.Store(false)
			return nil
		case <-time.After(pendingRequestTTL + time.Second):
		}

		requestsLock.Lock()
		sweepPendingRequestsLocked()
		nonEmpty := len(pendingRequests) > 0
		requestsLock.Unlock()

		// Reset flag before potential reschedule to avoid a gap where
		// a concurrent AwaitRequest could miss starting a new goroutine.
		cleanupScheduled.Store(false)
		if nonEmpty {
			scheduleCleanup()
		}
		return nil
	})
}

// sweepPendingRequestsLocked removes any pending requests that have exceeded
// the TTL. The caller must hold requestsLock.
func sweepPendingRequestsLocked() {
	now := time.Now()
	for key, r := range pendingRequests {
		if now.After(r.expiresAt) {
			delete(pendingRequests, key)
		}
	}
}

// clearPendingRequests removes all pending requests. Called on module stop.
func clearPendingRequests() {
	requestsLock.Lock()
	pendingRequests = make(map[string]*request)
	requestsLock.Unlock()
}

// consumeRequest retrieves and removes a pending request for the given address.
// Returns an error if the request has expired.
func consumeRequest(address string) (r *request, err error) {
	requestsLock.Lock()

	r, ok := pendingRequests[address]
	if ok {
		delete(pendingRequests, address)
		requestsLock.Unlock()
		if time.Now().After(r.expiresAt) {
			return nil, fmt.Errorf("pending request for %s has expired", address)
		}
		return r, nil
	}

	requestsLock.Unlock()
	return nil, fmt.Errorf("no pending request for %s", address)
}

// proxyDecider is called by the proxy for each new connection to determine the
// upstream destination and local binding parameters.
func proxyDecider(local net.Addr, peer net.Addr) (remoteIP net.IP, remotePort uint16, binding *proxy.LocalBinding, extraInfo any, err error) {
	r, err := consumeRequest(peer.String())
	if err != nil {
		return nil, 0, nil, nil, err
	}

	return r.connInfo.Entity.IP, uint16(r.connInfo.Entity.Port), &r.binding, r.connInfo, nil
}
