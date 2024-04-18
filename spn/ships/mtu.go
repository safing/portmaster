package ships

import "net"

// MTU Calculation Configuration.
const (
	BaseMTU           = 1460 // 1500 with 40 bytes extra space for special cases.
	IPv4HeaderMTUSize = 20   // Without options, as not common.
	IPv6HeaderMTUSize = 40   // Without options, as not common.
	TCPHeaderMTUSize  = 60   // Maximum size with options.
	UDPHeaderMTUSize  = 8    // Has no options.
)

func (ship *ShipBase) calculateLoadSize(ip net.IP, addr net.Addr, subtract ...int) {
	ship.loadSize = BaseMTU

	// Convert addr to IP if needed.
	if ip == nil && addr != nil {
		switch v := addr.(type) {
		case *net.TCPAddr:
			ip = v.IP
		case *net.UDPAddr:
			ip = v.IP
		case *net.IPAddr:
			ip = v.IP
		}
	}

	// Subtract IP Header, if IP is available.
	if ip != nil {
		if ip4 := ip.To4(); ip4 != nil {
			ship.loadSize -= IPv4HeaderMTUSize
		} else {
			ship.loadSize -= IPv6HeaderMTUSize
		}
	}

	// Subtract others.
	for sub := range subtract {
		ship.loadSize -= sub
	}

	// Raise buf size to at least load size.
	if ship.bufSize < ship.loadSize {
		ship.bufSize = ship.loadSize
	}
}
