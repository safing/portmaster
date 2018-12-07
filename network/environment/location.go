// Copyright Safing ICS Technologies GmbH. Use of this source code is governed by the AGPL license that can be found in the LICENSE file.

package environment

import (
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"time"

	"github.com/Safing/portmaster/network/netutils"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
)

// TODO: reference forking
// TODO: Create IPv6 version of GetApproximateInternetLocation

// GetApproximateInternetLocation returns the IP-address of the nearest ping-answering internet node
func GetApproximateInternetLocation() (net.IP, error) {
	// TODO: first check if we have a public IP
	// net.InterfaceAddrs()

	// Traceroute example

	var dst net.IPAddr
	dst.IP = net.IPv4(8, 8, 8, 8)

	c, err := net.ListenPacket("ip4:1", "0.0.0.0") // ICMP for IPv4
	if err != nil {
		return nil, err
	}
	defer c.Close()

	p := ipv4.NewPacketConn(c)
	err = p.SetControlMessage(ipv4.FlagTTL|ipv4.FlagSrc|ipv4.FlagDst|ipv4.FlagInterface, true)
	if err != nil {
		return nil, err
	}

	wm := icmp.Message{
		Type: ipv4.ICMPTypeEcho, Code: 0,
		Body: &icmp.Echo{
			ID: os.Getpid() & 0xffff,
			// TODO: think of something better and not suspicious
			Data: []byte("HELLO-R-U-THERE"),
		},
	}
	rb := make([]byte, 1500)

next:
	for i := 1; i <= 64; i++ { // up to 64 hops
	repeat:
		for j := 1; j <= 5; j++ {
			wm.Body.(*icmp.Echo).Seq = i

			wb, err := wm.Marshal(nil)
			if err != nil {
				return nil, err
			}

			err = p.SetTTL(i)
			if err != nil {
				return nil, err
			}

			_, err = p.WriteTo(wb, nil, &dst)
			if err != nil {
				return nil, err
			}

			err = p.SetReadDeadline(time.Now().Add(10 * time.Millisecond))
			if err != nil {
				return nil, err
			}

			// n, cm, peer, err := p.ReadFrom(rb)
			// readping:
			for {

				n, _, peer, err := p.ReadFrom(rb)
				if err != nil {
					if err, ok := err.(net.Error); ok && err.Timeout() {
						continue repeat
					}
					return nil, err
				}

				rm, err := icmp.ParseMessage(1, rb[:n])
				if err != nil {
					log.Fatal(err)
				}

				switch rm.Type {
				case ipv4.ICMPTypeTimeExceeded:
					ip := net.ParseIP(peer.String())
					if ip == nil {
						return nil, errors.New(fmt.Sprintf("failed to parse IP: %s", peer.String()))
					}
					if !netutils.IPIsLAN(ip) {
						return ip, nil
					}
					continue next
				case ipv4.ICMPTypeEchoReply:
					continue next
				default:
					// log.Tracef("unknown ICMP message: %+v\n", rm)
				}
			}
		}
	}
	return nil, nil
}
