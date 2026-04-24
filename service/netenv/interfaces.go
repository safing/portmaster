package netenv

import (
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/service/network/netutils"
)

// cachedNetInterface holds a network interface with its pre-parsed IP addresses.
type cachedNetInterface struct {
	iface     net.Interface
	addrs     []net.IP // all routable addresses; used for IP-based lookup
	macStr    string   // HardwareAddr.String() result, cached to avoid per-search allocations
	firstIPv4 net.IP   // first routable IPv4, or nil; cached to avoid scanning addrs on every call
	firstIPv6 net.IP   // first routable IPv6, or nil; cached to avoid scanning addrs on every call
}

// InterfaceInfo holds a matched network interface with the preferred
// IP addresses for each address family.
//
// When the interface was found by a specific IP, that IP is used as the
// preferred address for its address family; the first address of the
// other family (if any) is populated from the interface's address list.
// When the interface was found by name or MAC, the first address of each
// family from the address list is used.
type InterfaceInfo struct {
	Interface *net.Interface
	IPv4      net.IP // first routable IPv4 address for this interface; nil if none
	IPv6      net.IP // first routable IPv6 address for this interface; nil if none
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
		// Skip loopback — it is not useful for cross-host communication.
		if ifaces[i].Flags&net.FlagLoopback != 0 {
			continue
		}

		entry := cachedNetInterface{
			iface:  ifaces[i],
			macStr: ifaces[i].HardwareAddr.String(),
		}

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

		// Pre-cache the first address of each family so buildInterfaceInfo
		// can return them with two field reads instead of scanning addrs.
		for _, ip := range entry.addrs {
			if ip.To4() != nil {
				if entry.firstIPv4 == nil {
					entry.firstIPv4 = ip
				}
			} else {
				if entry.firstIPv6 == nil {
					entry.firstIPv6 = ip
				}
			}
			if entry.firstIPv4 != nil && entry.firstIPv6 != nil {
				break
			}
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

// buildInterfaceInfo constructs an InterfaceInfo from a cache entry.
// If knownIP is non-nil it is used as the preferred address for its
// address family; the pre-cached first address of the other family fills
// the remaining field. Both fields are read directly from the entry — no
// scan of addrs is needed.
func buildInterfaceInfo(entry *cachedNetInterface, knownIP net.IP) *InterfaceInfo {
	info := &InterfaceInfo{
		Interface: &entry.iface,
		IPv4:      entry.firstIPv4,
		IPv6:      entry.firstIPv6,
	}
	// Override the matched family with the exact IP used to find this entry.
	if knownIP != nil {
		if knownIP.To4() != nil {
			info.IPv4 = knownIP
		} else {
			info.IPv6 = knownIP
		}
	}
	return info
}

// GetInterface returns the local network interface identified by ifinfo.
// ifinfo may be an IP address, a MAC address, or an interface name; they are
// tried in that order. An error is returned when no interface matches.
func GetInterface(ifinfo string) (*InterfaceInfo, error) {
	// Fast path: concurrent reads when the cache is already valid.
	ifaceCacheLock.RLock()
	if cacheReady() {
		entry, matchedIP := searchByIfinfo(ifinfo)
		ifaceCacheLock.RUnlock()
		if entry == nil {
			return nil, fmt.Errorf("no interface found %q", ifinfo)
		}
		return buildInterfaceInfo(entry, matchedIP), nil
	}
	ifaceCacheLock.RUnlock()

	// Slow path: refresh the cache, then search.
	ifaceCacheLock.Lock()
	defer ifaceCacheLock.Unlock()
	if err := ensureIfaceCache(); err != nil {
		return nil, err
	}
	entry, matchedIP := searchByIfinfo(ifinfo)
	if entry == nil {
		return nil, fmt.Errorf("no interface found %q", ifinfo)
	}
	return buildInterfaceInfo(entry, matchedIP), nil
}

// searchByIfinfo searches ifaceCache in priority order: IP → MAC → name.
// It returns the matched cache entry and, when the match was by IP, the
// normalised IP that was used (so the caller can pin it as the preferred
// address for that family). The IP return value is nil for name/MAC matches.
// The caller must hold ifaceCacheLock (for reading or writing).
func searchByIfinfo(ifinfo string) (*cachedNetInterface, net.IP) {
	if ip := net.ParseIP(ifinfo); ip != nil {
		normalized := normalizeIP(ip)
		return searchIfaceByIP(normalized), normalized
	}
	if mac, err := net.ParseMAC(ifinfo); err == nil {
		return searchIfaceByMAC(mac.String()), nil
	}
	return searchIfaceByName(ifinfo), nil
}

// GetInterfaceByIP returns the local network interface that has ip assigned.
func GetInterfaceByIP(ip net.IP) (*InterfaceInfo, error) {
	if ip == nil {
		return nil, fmt.Errorf("GetInterfaceByIP called with nil IP")
	}
	normalized := normalizeIP(ip)

	ifaceCacheLock.RLock()
	if cacheReady() {
		entry := searchIfaceByIP(normalized)
		ifaceCacheLock.RUnlock()
		if entry == nil {
			return nil, fmt.Errorf("no interface found with IP %s", ip)
		}
		return buildInterfaceInfo(entry, normalized), nil
	}
	ifaceCacheLock.RUnlock()

	ifaceCacheLock.Lock()
	defer ifaceCacheLock.Unlock()
	if err := ensureIfaceCache(); err != nil {
		return nil, err
	}
	if entry := searchIfaceByIP(normalized); entry != nil {
		return buildInterfaceInfo(entry, normalized), nil
	}
	return nil, fmt.Errorf("no interface found with IP %s", ip)
}

// GetInterfaceByMAC returns the local network interface with the given hardware address.
func GetInterfaceByMAC(mac net.HardwareAddr) (*InterfaceInfo, error) {
	macStr := mac.String()

	ifaceCacheLock.RLock()
	if cacheReady() {
		entry := searchIfaceByMAC(macStr)
		ifaceCacheLock.RUnlock()
		if entry == nil {
			return nil, fmt.Errorf("no interface found with MAC %s", mac)
		}
		return buildInterfaceInfo(entry, nil), nil
	}
	ifaceCacheLock.RUnlock()

	ifaceCacheLock.Lock()
	defer ifaceCacheLock.Unlock()
	if err := ensureIfaceCache(); err != nil {
		return nil, err
	}
	if entry := searchIfaceByMAC(macStr); entry != nil {
		return buildInterfaceInfo(entry, nil), nil
	}
	return nil, fmt.Errorf("no interface found with MAC %s", mac)
}

// GetInterfaceByName returns the local network interface with the given name.
func GetInterfaceByName(name string) (*InterfaceInfo, error) {
	ifaceCacheLock.RLock()
	if cacheReady() {
		entry := searchIfaceByName(name)
		ifaceCacheLock.RUnlock()
		if entry == nil {
			return nil, fmt.Errorf("no interface found with name %q", name)
		}
		return buildInterfaceInfo(entry, nil), nil
	}
	ifaceCacheLock.RUnlock()

	ifaceCacheLock.Lock()
	defer ifaceCacheLock.Unlock()
	if err := ensureIfaceCache(); err != nil {
		return nil, err
	}
	if entry := searchIfaceByName(name); entry != nil {
		return buildInterfaceInfo(entry, nil), nil
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

// searchIfaceByIP returns the cache entry whose address list contains ip, or nil.
// The caller must hold ifaceCacheLock for reading or writing.
func searchIfaceByIP(ip net.IP) *cachedNetInterface {
	for i := range ifaceCache {
		for _, addr := range ifaceCache[i].addrs {
			if ip.Equal(addr) {
				return &ifaceCache[i]
			}
		}
	}
	return nil
}

// searchIfaceByMAC returns the cache entry whose hardware address matches
// macStr (in canonical net.HardwareAddr.String() form), or nil.
// The caller must hold ifaceCacheLock for reading or writing.
func searchIfaceByMAC(macStr string) *cachedNetInterface {
	for i := range ifaceCache {
		if ifaceCache[i].macStr == macStr {
			return &ifaceCache[i]
		}
	}
	return nil
}

// searchIfaceByName returns the cache entry with the given name, or nil.
// The caller must hold ifaceCacheLock for reading or writing.
func searchIfaceByName(name string) *cachedNetInterface {
	for i := range ifaceCache {
		if ifaceCache[i].iface.Name == name {
			return &ifaceCache[i]
		}
	}
	return nil
}

// ---- Physical default interface ----

// PhysicalDefaultInterfaces holds the best physical network adapter per IP
// family, together with its preferred bind addresses. IPv4 and IPv6 may
// resolve to different interfaces — for example when a VPN tunnels only IPv4
// and IPv6 traffic exits directly on Ethernet, or when Ethernet serves IPv4
// and a mobile hotspot provides IPv6.
// A nil field means no physical interface with a default route for that family
// was found (e.g. IPv6 is simply not configured on this host).
type PhysicalDefaultInterfaces struct {
	ForIPv4 *InterfaceInfo // best physical adapter handling the default IPv4 route; nil if none
	ForIPv6 *InterfaceInfo // best physical adapter handling the default IPv6 route; nil if none
}

var (
	physicalDefaultIfacesCache            PhysicalDefaultInterfaces
	physicalDefaultIfacesCacheValid       bool
	physicalDefaultIfacesLock             sync.RWMutex
	physicalDefaultIfacesChangedFlag      = GetNetworkChangedFlag()
	physicalDefaultIfacesDontRefreshUntil time.Time
)

// GetBestPhysicalDefaultInterfaces returns the physical network adapters
// (Ethernet, WiFi, mobile broadband) currently used for internet traffic,
// one per IP family. VPN, tunnel, and other virtual interfaces are explicitly
// excluded, making the result safe to use as split-tunnel bypass targets.
//
// Selection criteria per family (all must be satisfied):
//   - Adapter type is physical hardware (Ethernet, WiFi, mobile broadband).
//   - Has at least one routable unicast address for that family (not link-local).
//   - Has a default route (0.0.0.0/0 or ::/0) in the routing table.
//   - When multiple candidates qualify, the one with the lowest route metric wins.
//
// The result is cached and refreshed only when a network change is detected,
// making it safe to call on every new connection without performance overhead.
func GetBestPhysicalDefaultInterfaces() (PhysicalDefaultInterfaces, error) {
	// Fast path: concurrent reads when cache is valid.
	physicalDefaultIfacesLock.RLock()
	if physicalDefaultIfacesCacheValid && !physicalDefaultIfacesChangedFlag.IsSet() {
		result := physicalDefaultIfacesCache
		physicalDefaultIfacesLock.RUnlock()
		return result, nil
	}
	physicalDefaultIfacesLock.RUnlock()

	// Slow path: refresh under write lock.
	physicalDefaultIfacesLock.Lock()
	defer physicalDefaultIfacesLock.Unlock()

	// Re-check: another goroutine may have refreshed while we waited.
	if physicalDefaultIfacesCacheValid && !physicalDefaultIfacesChangedFlag.IsSet() {
		return physicalDefaultIfacesCache, nil
	}

	// Throttle: if a refresh just ran, return the cached result even if the
	// change flag fired again — avoids hammering the OS during interface churn.
	if physicalDefaultIfacesCacheValid && time.Now().Before(physicalDefaultIfacesDontRefreshUntil) {
		return physicalDefaultIfacesCache, nil
	}
	physicalDefaultIfacesDontRefreshUntil = time.Now().Add(1 * time.Second)

	// Consume the change flag before the (potentially slow) platform call so
	// any change that arrives during the call will trigger a re-evaluation.
	physicalDefaultIfacesChangedFlag.Refresh()

	ipv4Iface, ipv6Iface, err := selectPhysicalDefaultInterfaces()
	if err != nil {
		physicalDefaultIfacesCacheValid = false
		return PhysicalDefaultInterfaces{}, err
	}
	result := PhysicalDefaultInterfaces{
		ForIPv4: interfaceToInfo(ipv4Iface),
		ForIPv6: interfaceToInfo(ipv6Iface),
	}
	physicalDefaultIfacesCache = result
	physicalDefaultIfacesCacheValid = true
	return result, nil
}

// interfaceToInfo looks up iface in the interface cache and returns an
// InterfaceInfo populated with the first routable address per family.
// Falls back to scanning iface.Addrs() directly when the cache is unavailable
// (e.g. a transient failure during network churn), so IPv4/IPv6 are always
// populated when addresses exist on the interface.
// The caller must NOT hold ifaceCacheLock.
func interfaceToInfo(iface *net.Interface) *InterfaceInfo {
	if iface == nil {
		return nil
	}
	ifaceCacheLock.RLock()
	if cacheReady() {
		entry := searchIfaceByName(iface.Name)
		ifaceCacheLock.RUnlock()
		if entry != nil {
			return buildInterfaceInfo(entry, nil)
		}
		// Interface not in cache yet (added after last refresh); fall through.
	} else {
		ifaceCacheLock.RUnlock()

		ifaceCacheLock.Lock()
		if err := ensureIfaceCache(); err == nil {
			if entry := searchIfaceByName(iface.Name); entry != nil {
				result := buildInterfaceInfo(entry, nil)
				ifaceCacheLock.Unlock()
				return result
			}
		}
		ifaceCacheLock.Unlock()
	}

	// Cache unavailable or interface not present in it — populate addresses
	// directly from the kernel so IPv4/IPv6 are never silently nil.
	return buildInterfaceInfoDirect(iface)
}

// buildInterfaceInfoDirect constructs an InterfaceInfo by calling iface.Addrs()
// directly, without using the cache. Used as a fallback when the cache is
// unavailable. It mirrors the address-selection logic of refreshIfaceCache.
func buildInterfaceInfoDirect(iface *net.Interface) *InterfaceInfo {
	info := &InterfaceInfo{Interface: iface}
	addrs, err := iface.Addrs()
	if err != nil {
		return info
	}
	for _, addr := range addrs {
		var ip net.IP
		switch v := addr.(type) {
		case *net.IPNet:
			ip = v.IP
		case *net.IPAddr:
			ip = v.IP
		}
		if ip == nil {
			continue
		}
		if ip4 := ip.To4(); ip4 != nil {
			if info.IPv4 == nil && !ip4.IsUnspecified() && !ip4.IsLoopback() && !ip4.IsLinkLocalUnicast() {
				info.IPv4 = ip4
			}
		} else {
			if info.IPv6 == nil && !ip.IsUnspecified() && !ip.IsLoopback() && !ip.IsLinkLocalUnicast() {
				info.IPv6 = ip
			}
		}
		if info.IPv4 != nil && info.IPv6 != nil {
			break
		}
	}
	return info
}

// hasRoutableIPv4 reports whether iface has at least one unicast IPv4 address
// that is globally routable — not unspecified (0.0.0.0), loopback (127.x.x.x),
// or link-local/APIPA (169.254.x.x).
//
// An interface may be physically present and have a default route while still
// lacking a usable IP (DHCP not completed, cable just reconnected, etc.).
// Checking the address is the final confirmation that the interface can
// actually forward packets.
func hasRoutableIPv4(iface *net.Interface) bool {
	addrs, err := iface.Addrs()
	if err != nil {
		return false
	}
	for _, addr := range addrs {
		var ip net.IP
		switch v := addr.(type) {
		case *net.IPNet:
			ip = v.IP
		case *net.IPAddr:
			ip = v.IP
		}
		ip4 := ip.To4()
		if ip4 == nil {
			continue
		}
		if !ip4.IsUnspecified() && !ip4.IsLoopback() && !ip4.IsLinkLocalUnicast() {
			return true
		}
	}
	return false
}

// hasRoutableIPv6 reports whether iface has at least one unicast IPv6 address
// that is globally routable — not unspecified (::), loopback (::1),
// or link-local (fe80::/10).
func hasRoutableIPv6(iface *net.Interface) bool {
	addrs, err := iface.Addrs()
	if err != nil {
		return false
	}
	for _, addr := range addrs {
		var ip net.IP
		switch v := addr.(type) {
		case *net.IPNet:
			ip = v.IP
		case *net.IPAddr:
			ip = v.IP
		}
		// Skip IPv4 addresses and nil.
		if ip == nil || ip.To4() != nil {
			continue
		}
		if !ip.IsUnspecified() && !ip.IsLoopback() && !ip.IsLinkLocalUnicast() {
			return true
		}
	}
	return false
}

// errNoPhysicalDefaultInterface is returned by unsupported platform stubs.
var errNoPhysicalDefaultInterface = errors.New("physical network interface detection is not supported on this platform")
