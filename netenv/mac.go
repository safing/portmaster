package netenv

import (
	"bytes"
	"net"
)

var (
	// UnspecifiedHardwareAddr defines the "unspecified" MAC address.
	UnspecifiedHardwareAddr = net.HardwareAddr{0x00, 0x00, 0x00, 0x00, 0x00, 0x00}

	// virtualMACPrefixes holds MAC address prefixes specifically linked to
	// virtualization solutions.
	virtualMACPrefixes = []net.HardwareAddr{
		// Mixed
		{0x00, 0x16, 0x3E}, // Xen
		{0x00, 0x18, 0x51}, // SWsoft
		{0x00, 0x1C, 0x42}, // Parallels Virtual Machine
		{0x02, 0x42},       // Docker
		{0x50, 0x6B, 0x8D}, // Nutanix AHV
		{0x52, 0x54, 0x00}, // KVM VMs
		{0x54, 0x52},       // KVM (proxmox)
		{0x58, 0x9C, 0xFC}, // bhyve by FreebsdF

		// Microsoft
		{0x00, 0x03, 0xFF}, // Microsoft Virtual PC
		{0x00, 0x15, 0x5D}, // Hyper-V (by Microsoft for VMs)
		{0x00, 0x1D, 0xD8}, // Microsoft SCVMM (actual space at 00:1D:D8:B7:1C:00 - 00:1D:D8:F4:1F:FF)

		// Oracle
		{0x00, 0x0F, 0x4B}, // Oracle Virtual Iron 4
		{0x00, 0x14, 0x4F}, // Oracle VM Server for SPARC
		{0x00, 0x21, 0xF6}, // Oracle VirtualBox 3.3
		{0x08, 0x00, 0x27}, // Oracle VirtualBox 5.2
		{0x52, 0x54, 0x00}, // Oracle VirtualBox 5.2

		// VMWare
		{0x00, 0x05, 0x69}, // VMWare ESX, GSX Server
		{0x00, 0x0C, 0x29}, // VMWare vSphere, Workstation, Horizon
		{0x00, 0x1C, 0x14}, // VMWare
		{0x00, 0x50, 0x56}, // VMWare vSphere, Workstation, ESX Server
	}

	// Other available sources:
	// https://standards-oui.ieee.org/oui/oui.txt
	// https://gitlab.com/wireshark/wireshark/-/raw/master/manuf
	// https://macaddress.io/faq

	// This website has some detection for special use cases, such as docker:
	// https://maclookup.app/

	// macAddressUniversalLocalBit defines whether the address is universally or
	// locally administered address.
	macAddressUniversalLocalBit byte = 0x02
)

// HardwareAddressIsLocal returns whether the given HardwareAddr is part of a
// localhost-only network with a certain probability.
// If checkLocalBit is enabled, true is returned when the local bit of the
// address is set. This is might lead to many false positivies, but is a
// helpful workaround or quick fix.
func HardwareAddressIsLocal(hwAddr net.HardwareAddr, checkLocalBit bool) bool {
	// Check input.
	if len(hwAddr) == 0 {
		return false
	}

	// Check for the Universal/Local bit.
	if checkLocalBit && hwAddr[0]&macAddressUniversalLocalBit == macAddressUniversalLocalBit {
		return true
	}

	// Check if any of the virtual prefixes match.
	for _, prefix := range virtualMACPrefixes {
		if bytes.HasPrefix(hwAddr, prefix) {
			return true
		}
	}

	return false
}
