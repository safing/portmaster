package netenv

import (
	"errors"
	"fmt"
	"net"
	"syscall"
	"time"

	"golang.org/x/net/ipv4"

	"golang.org/x/net/icmp"

	"github.com/safing/portbase/log"
	"github.com/safing/portbase/rng"
	"github.com/safing/portmaster/network/netutils"
)

var (
	locationTestingIPv4     = "1.1.1.1"
	locationTestingIPv4Addr *net.IPAddr
)

func prepLocation() (err error) {
	locationTestingIPv4Addr, err = net.ResolveIPAddr("ip", locationTestingIPv4)
	return err
}

// GetApproximateInternetLocation returns the nearest detectable IP address. If one or more global IP addresses are configured, one of them is returned. Currently only support IPv4. Else, the IP address of the nearest ping-answering internet node is returned.
func GetApproximateInternetLocation() (net.IP, error) { //nolint:gocognit
	// TODO: Create IPv6 version of GetApproximateInternetLocation

	// First check if we have an assigned IPv6 address. Return that if available.
	globalIPv4, _, err := GetAssignedGlobalAddresses()
	if err != nil {
		log.Warningf("netenv: location approximation: failed to get assigned global addresses: %s", err)
	} else if len(globalIPv4) > 0 {
		return globalIPv4[0], nil
	}

	// Create OS specific ICMP Listener.
	conn, err := newICMPListener(locationTestingIPv4)
	if err != nil {
		return nil, fmt.Errorf("failed to listen: %s", err)
	}
	defer conn.Close()
	v4Conn := ipv4.NewPacketConn(conn)

	// Generate a random ID for the ICMP packets.
	msgID, err := rng.Number(0xFFFF) // uint16
	if err != nil {
		return nil, fmt.Errorf("failed to generate ID: %s", err)
	}

	// Create ICMP message body
	pingMessage := icmp.Message{
		Type: ipv4.ICMPTypeEcho,
		Code: 0,
		Body: &icmp.Echo{
			ID:   int(msgID),
			Seq:  0, // increased before marshal
			Data: []byte{},
		},
	}
	recvBuffer := make([]byte, 1500)
	maxHops := 4 // add one for every reply that is not global

next:
	for i := 1; i <= maxHops; i++ {
	repeat:
		for j := 1; j <= 2; j++ { // Try every hop twice.
			// Increase sequence number.
			pingMessage.Body.(*icmp.Echo).Seq++

			// Make packet data.
			pingPacket, err := pingMessage.Marshal(nil)
			if err != nil {
				return nil, err
			}

			// Set TTL on IP packet.
			err = v4Conn.SetTTL(i)
			if err != nil {
				return nil, err
			}

			// Send ICMP packet.
			if _, err := conn.WriteTo(pingPacket, locationTestingIPv4Addr); err != nil {
				if neterr, ok := err.(*net.OpError); ok {
					if neterr.Err == syscall.ENOBUFS {
						continue
					}
				}
				return nil, err
			}

			// Listen for replies to the ICMP packet.
		listen:
			for {
				// Set read timeout.
				err = conn.SetReadDeadline(
					time.Now().Add(
						time.Duration(i*2+30) * time.Millisecond,
					),
				)
				if err != nil {
					return nil, err
				}

				// Read next packet.
				n, src, err := conn.ReadFrom(recvBuffer)
				if err != nil {
					if err, ok := err.(net.Error); ok && err.Timeout() {
						// Continue with next packet if we timeout
						continue repeat
					}
					return nil, err
				}

				// Parse remote IP address.
				addr, ok := src.(*net.IPAddr)
				if !ok {
					return nil, fmt.Errorf("failed to parse IP: %s", src.String())
				}

				// Continue if we receive a packet from ourself. This is specific to Windows.
				if me, err := IsMyIP(addr.IP); err == nil && me {
					log.Tracef("netenv: location approximation: ignoring own message from %s", src)
					continue listen
				}

				// If we received something from a global IP address, we have succeeded and can return immediately.
				if netutils.GetIPScope(addr.IP).IsGlobal() {
					return addr.IP, nil
				}

				// For everey non-global reply received, increase the maximum hops to try.
				maxHops++

				// Parse the ICMP message.
				icmpReply, err := icmp.ParseMessage(1, recvBuffer[:n])
				if err != nil {
					log.Warningf("netenv: location approximation: failed to parse ICMP message: %s", err)
					continue listen
				}

				// React based on message type.
				switch icmpReply.Type {
				case ipv4.ICMPTypeTimeExceeded, ipv4.ICMPTypeEchoReply:
					log.Tracef("netenv: location approximation: receveived %q from %s", icmpReply.Type, addr.IP)
					continue next
				case ipv4.ICMPTypeDestinationUnreachable:
					return nil, fmt.Errorf("destination unreachable")
				default:
					log.Tracef("netenv: location approximation: unexpected ICMP reply: received %q from %s", icmpReply.Type, addr.IP)
				}
			}
		}
	}

	return nil, errors.New("no usable response to any icmp message")
}
