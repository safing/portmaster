package netenv

import (
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/service/network/netutils"
)

// cachedNetInterface holds a network interface with its pre-parsed IP addresses.
type cachedNetInterface struct {
	iface net.Interface
	addrs []net.IP
}

var (
	// ifaceCache stores the latest enumerated network interfaces as a slice.
	// A slice is used instead of maps because a typical host has only a handful
	// of interfaces (2–10). Linear scans over such small slices are faster than
	// map lookups: no hashing, no bucket pointer chasing, and the data fits
	// entirely in a few cache lines. Maps would also require three separate
	// structures (by name, IP, MAC), adding allocation and maintenance cost with
	// no measurable benefit at real-world sizes.
	// It is nil until the first call to any GetInterface* function (lazy init).
	ifaceCache                 []cachedNetInterface
	ifaceCacheLock             sync.RWMutex
	ifaceCacheChangedFlag      = GetNetworkChangedFlag()
	ifaceCacheRefreshError     error //nolint:errname // Not what the linter thinks this is for.
	ifaceCacheDontRefreshUntil time.Time
)

// refreshIfaceCache re-enumerates all network interfaces and stores them in ifaceCache.
// It also resets the network-changed flag.
// Refreshes are throttled to at most once per second to avoid redundant
// re-enumerations during rapid interface churn (e.g. network reconnects).
// The caller must hold ifaceCacheLock for writing.
func refreshIfaceCache() error {
	// Throttle: return early if we refreshed very recently; the existing cache remains valid.
	if time.Now().Before(ifaceCacheDontRefreshUntil) {
		if ifaceCacheRefreshError != nil {
			return fmt.Errorf("failed to previously refresh interface cache: %w", ifaceCacheRefreshError)
		}
		return nil
	}
	ifaceCacheRefreshError = nil
	ifaceCacheDontRefreshUntil = time.Now().Add(1 * time.Second)

	ifaces, err := net.Interfaces()
	if err != nil {
		ifaceCacheRefreshError = err
		return fmt.Errorf("failed to enumerate network interfaces: %w", err)
	}

	newCache := make([]cachedNetInterface, 0, len(ifaces))
	for i := range ifaces {
		// Skip interfaces that are down — they have no usable IP connectivity.
		if ifaces[i].Flags&net.FlagUp == 0 {
			continue
		}

		entry := cachedNetInterface{iface: ifaces[i]}

		addrs, addrErr := ifaces[i].Addrs()
		if addrErr != nil {
			log.Warningf("netenv: failed to get addresses for interface %s: %v", ifaces[i].Name, addrErr)
		} else {
			for _, addr := range addrs {
				var ip net.IP
				switch v := addr.(type) {
				case *net.IPNet:
					ip = v.IP
				case *net.IPAddr:
					ip = v.IP
				}
				// Skip addresses of unexpected types (switch default left ip nil).
				if ip == nil {
					continue
				}
				// Use the 4-byte form for IPv4 so it matches what was stored during cache build.
				if ip4 := ip.To4(); ip4 != nil {
					ip = ip4
				}
				// Skip link-local addresses (169.254.x.x / fe80::) — they are
				// non-routable and cannot be used for tunneling to remote hosts.
				if netutils.GetIPScope(ip) == netutils.LinkLocal {
					continue
				}
				entry.addrs = append(entry.addrs, ip)
			}
		}

		// Skip interfaces with no usable unicast addresses — they cannot
		// participate in normal IP connectivity and are not searchable by IP.
		if len(entry.addrs) == 0 {
			continue
		}

		newCache = append(newCache, entry)
	}

	ifaceCache = newCache
	ifaceCacheChangedFlag.Refresh()

	return nil
}

// ensureIfaceCache guarantees the cache is populated and up to date.
// The caller must hold ifaceCacheLock for writing.
func ensureIfaceCache() error {
	if ifaceCache == nil || ifaceCacheChangedFlag.IsSet() {
		return refreshIfaceCache()
	}
	return nil
}

// cacheReady reports whether the cache is populated and current.
// The caller must hold at least ifaceCacheLock for reading.
func cacheReady() bool {
	return ifaceCache != nil && !ifaceCacheChangedFlag.IsSet()
}

// GetInterface returns the local network interface identified by ifinfo.
// ifinfo may be an IP address, a MAC address, or an interface name; they are
// tried in that order. An error is returned when no interface matches.
func GetInterface(ifinfo string) (*net.Interface, error) {
	// Fast path: concurrent reads when the cache is already valid.
	ifaceCacheLock.RLock()
	if cacheReady() {
		iface := searchByIfinfo(ifinfo)
		ifaceCacheLock.RUnlock()
		if iface == nil {
			return nil, fmt.Errorf("no interface found for %q", ifinfo)
		}
		return iface, nil
	}
	ifaceCacheLock.RUnlock()

	// Slow path: refresh the cache, then search.
	ifaceCacheLock.Lock()
	defer ifaceCacheLock.Unlock()
	if err := ensureIfaceCache(); err != nil {
		return nil, err
	}
	iface := searchByIfinfo(ifinfo)
	if iface == nil {
		return nil, fmt.Errorf("no interface found for %q", ifinfo)
	}
	return iface, nil
}

// searchByIfinfo searches ifaceCache in priority order: IP → MAC → name.
// The caller must hold ifaceCacheLock (for reading or writing).
func searchByIfinfo(ifinfo string) *net.Interface {
	if ip := net.ParseIP(ifinfo); ip != nil {
		return searchIfaceByIP(normalizeIP(ip))
	}
	if mac, err := net.ParseMAC(ifinfo); err == nil {
		return searchIfaceByMAC(mac.String())
	}
	return searchIfaceByName(ifinfo)
}

// GetInterfaceByIP returns the local network interface that has ip assigned.
func GetInterfaceByIP(ip net.IP) (*net.Interface, error) {
	if ip == nil {
		return nil, fmt.Errorf("GetInterfaceByIP called with nil IP")
	}
	normalized := normalizeIP(ip)

	ifaceCacheLock.RLock()
	if cacheReady() {
		iface := searchIfaceByIP(normalized)
		ifaceCacheLock.RUnlock()
		if iface == nil {
			return nil, fmt.Errorf("no interface found with IP %s", ip)
		}
		return iface, nil
	}
	ifaceCacheLock.RUnlock()

	ifaceCacheLock.Lock()
	defer ifaceCacheLock.Unlock()
	if err := ensureIfaceCache(); err != nil {
		return nil, err
	}
	if iface := searchIfaceByIP(normalized); iface != nil {
		return iface, nil
	}
	return nil, fmt.Errorf("no interface found with IP %s", ip)
}

// GetInterfaceByMAC returns the local network interface with the given hardware address.
func GetInterfaceByMAC(mac net.HardwareAddr) (*net.Interface, error) {
	macStr := mac.String()

	ifaceCacheLock.RLock()
	if cacheReady() {
		iface := searchIfaceByMAC(macStr)
		ifaceCacheLock.RUnlock()
		if iface == nil {
			return nil, fmt.Errorf("no interface found with MAC %s", mac)
		}
		return iface, nil
	}
	ifaceCacheLock.RUnlock()

	ifaceCacheLock.Lock()
	defer ifaceCacheLock.Unlock()
	if err := ensureIfaceCache(); err != nil {
		return nil, err
	}
	if iface := searchIfaceByMAC(macStr); iface != nil {
		return iface, nil
	}
	return nil, fmt.Errorf("no interface found with MAC %s", mac)
}

// GetInterfaceByName returns the local network interface with the given name.
func GetInterfaceByName(name string) (*net.Interface, error) {
	ifaceCacheLock.RLock()
	if cacheReady() {
		iface := searchIfaceByName(name)
		ifaceCacheLock.RUnlock()
		if iface == nil {
			return nil, fmt.Errorf("no interface found with name %q", name)
		}
		return iface, nil
	}
	ifaceCacheLock.RUnlock()

	ifaceCacheLock.Lock()
	defer ifaceCacheLock.Unlock()
	if err := ensureIfaceCache(); err != nil {
		return nil, err
	}
	if iface := searchIfaceByName(name); iface != nil {
		return iface, nil
	}
	return nil, fmt.Errorf("no interface found with name %q", name)
}

// normalizeIP returns the 4-byte form of an IPv4 address, or the IP unchanged
// for IPv6. This matches the form stored in cachedNetInterface.addrs.
func normalizeIP(ip net.IP) net.IP {
	if ip4 := ip.To4(); ip4 != nil {
		return ip4
	}
	return ip
}

// searchIfaceByIP returns the interface that owns ip, or nil.
// The caller must hold ifaceCacheLock for reading or writing.
func searchIfaceByIP(ip net.IP) *net.Interface {
	for i := range ifaceCache {
		for _, addr := range ifaceCache[i].addrs {
			if ip.Equal(addr) {
				return &ifaceCache[i].iface
			}
		}
	}
	return nil
}

// searchIfaceByMAC returns the interface whose hardware address matches
// macStr (in canonical net.HardwareAddr.String() form), or nil.
// The caller must hold ifaceCacheLock for reading or writing.
func searchIfaceByMAC(macStr string) *net.Interface {
	for i := range ifaceCache {
		if ifaceCache[i].iface.HardwareAddr.String() == macStr {
			return &ifaceCache[i].iface
		}
	}
	return nil
}

// searchIfaceByName returns the interface with the given name, or nil.
// The caller must hold ifaceCacheLock for reading or writing.
func searchIfaceByName(name string) *net.Interface {
	for i := range ifaceCache {
		if ifaceCache[i].iface.Name == name {
			return &ifaceCache[i].iface
		}
	}
	return nil
}
