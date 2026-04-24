//go:build windows

package netenv

import (
	"fmt"
	"net"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Windows IANA ifType constants.
// https://www.iana.org/assignments/ianaiftype-mib/ianaiftype-mib
//
// Only types that represent real physical hardware used for internet access
// are listed. Types that look physical but are excluded with justification:
//   - IF_TYPE_GIGABITETHERNET (117): Windows drivers report GbE/10GbE as
//     ETHERNET_CSMACD (6) at the NDIS level; 117 is never seen in practice.
//   - IF_TYPE_PPP (23): shared by both dial-up modems and PPTP/PPPoE VPNs —
//     too ambiguous to include safely.
//   - IF_TYPE_USB (160): USB Ethernet dongles register as ETHERNET_CSMACD (6)
//     after the NDIS miniport wraps them; the USB type is not exposed here.
const (
	ifTypeEthernetCSMACD uint32 = 6   // 802.3 wired Ethernet (also used for GbE, 10GbE, USB dongles)
	ifTypeIEEE80211      uint32 = 71  // 802.11 WiFi
	ifTypeIEEE8023ADLag  uint32 = 161 // 802.3ad link aggregation / NIC teaming
	ifTypeIEEE80216WMAN  uint32 = 237 // WiMAX fixed wireless
	ifTypeWWANPP         uint32 = 243 // mobile broadband — GSM/LTE/5G
	ifTypeWWANPP2        uint32 = 244 // mobile broadband — CDMA
)

// selectPhysicalDefaultInterfaces calls GetAdaptersAddresses once with
// AF_UNSPEC to enumerate all adapters for both IP families in a single kernel
// call. The gateway list (FirstGatewayAddress) contains entries for all
// families; each entry's SocketAddress family field distinguishes IPv4 from
// IPv6. Both Ipv4Metric/IfIndex and Ipv6Metric/Ipv6IfIndex are populated in
// a single AF_UNSPEC response, so no second call is needed.
//
// Physical detection: Windows reports the adapter type via IfType. VPN and
// tunnel drivers always register as IF_TYPE_TUNNEL (131), IF_TYPE_PPP (23),
// IF_TYPE_OTHER (1), or similar non-physical types — never as Ethernet or
// WiFi — so this filter is reliable against any VPN software.
func selectPhysicalDefaultInterfaces() (*net.Interface, *net.Interface, error) {
	adapters, err := getAdapterAddresses()
	if err != nil {
		return nil, nil, err
	}

	var ipv4Iface, ipv6Iface *net.Interface
	var bestV4Metric, bestV6Metric uint32

	for a := adapters; a != nil; a = a.Next {
		if !isPhysicalIfType(a.IfType) {
			continue
		}

		// Walk the gateway list once and record which families have a gateway.
		hasV4Gateway, hasV6Gateway := false, false
		for gw := a.FirstGatewayAddress; gw != nil; gw = gw.Next {
			switch gw.Address.Sockaddr.Addr.Family {
			case windows.AF_INET:
				hasV4Gateway = true
			case windows.AF_INET6:
				hasV6Gateway = true
			}
			if hasV4Gateway && hasV6Gateway {
				break
			}
		}

		// IPv4 candidate: needs a gateway, a valid index, and a routable address.
		if hasV4Gateway && a.IfIndex != 0 {
			if iface, err := net.InterfaceByIndex(int(a.IfIndex)); err == nil && hasRoutableIPv4(iface) {
				if ipv4Iface == nil || a.Ipv4Metric < bestV4Metric {
					ipv4Iface = iface
					bestV4Metric = a.Ipv4Metric
				}
			}
		}

		// IPv6 candidate: needs a gateway, a valid index, and a routable address.
		if hasV6Gateway && a.Ipv6IfIndex != 0 {
			if iface, err := net.InterfaceByIndex(int(a.Ipv6IfIndex)); err == nil && hasRoutableIPv6(iface) {
				if ipv6Iface == nil || a.Ipv6Metric < bestV6Metric {
					ipv6Iface = iface
					bestV6Metric = a.Ipv6Metric
				}
			}
		}
	}

	return ipv4Iface, ipv6Iface, nil
}

// isPhysicalIfType reports whether the Windows interface type corresponds to
// real hardware. VPN and tunnel adapters always use non-physical type values.
func isPhysicalIfType(ifType uint32) bool {
	switch ifType {
	case ifTypeEthernetCSMACD, ifTypeIEEE80211, ifTypeIEEE8023ADLag,
		ifTypeIEEE80216WMAN, ifTypeWWANPP, ifTypeWWANPP2:
		return true
	}
	return false
}

// getAdapterAddresses calls GetAdaptersAddresses with AF_UNSPEC and
// GAA_FLAG_INCLUDE_GATEWAYS, returning adapters for all address families in
// one kernel call. It retries with an enlarged buffer if the OS signals that
// the initial 15 KB estimate was too small.
func getAdapterAddresses() (*windows.IpAdapterAddresses, error) {
	// 15 KB covers the vast majority of machines (typically < 2 KB per adapter).
	size := uint32(15000)
	for {
		buf := make([]byte, size)
		head := (*windows.IpAdapterAddresses)(unsafe.Pointer(&buf[0]))
		err := windows.GetAdaptersAddresses(
			windows.AF_UNSPEC,
			windows.GAA_FLAG_INCLUDE_GATEWAYS,
			0,
			head,
			&size,
		)
		if err == windows.ERROR_BUFFER_OVERFLOW {
			// size has been updated to the required value; retry.
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("GetAdaptersAddresses: %w", err)
		}
		return head, nil
	}
}
