package ships

import (
	"crypto/sha1"
	"net"

	"github.com/mr-tron/base58"
	"github.com/tevino/abool"
)

var (
	maskingEnabled = abool.New()
	maskingActive  = abool.New()
	maskingBytes   []byte
)

// EnableMasking enables masking with the given salt.
func EnableMasking(salt []byte) {
	if maskingEnabled.SetToIf(false, true) {
		maskingBytes = salt
		maskingActive.Set()
	}
}

// MaskAddress masks the given address if masking is enabled and the ship is
// not public.
func (ship *ShipBase) MaskAddress(addr net.Addr) string {
	// Return in plain if masking is not enabled or if ship is public.
	if maskingActive.IsNotSet() || ship.Public() {
		return addr.String()
	}

	switch typedAddr := addr.(type) {
	case *net.TCPAddr:
		return ship.MaskIP(typedAddr.IP)
	case *net.UDPAddr:
		return ship.MaskIP(typedAddr.IP)
	default:
		return ship.Mask([]byte(addr.String()))
	}
}

// MaskIP masks the given IP if masking is enabled and the ship is not public.
func (ship *ShipBase) MaskIP(ip net.IP) string {
	// Return in plain if masking is not enabled or if ship is public.
	if maskingActive.IsNotSet() || ship.Public() {
		return ip.String()
	}

	return ship.Mask(ip)
}

// Mask masks the given value.
func (ship *ShipBase) Mask(value []byte) string {
	// Hash the IP with masking bytes.
	hasher := sha1.New() //nolint:gosec // Not used for cryptography.
	hasher.Write(maskingBytes)
	hasher.Write(value)
	masked := hasher.Sum(nil)

	// Return first 8 characters from the base58-encoded hash.
	return "masked:" + base58.Encode(masked)[:8]
}
