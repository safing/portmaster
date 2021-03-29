package netenv

import (
	"errors"
	"net"
	"sync"
	"syscall"
	"time"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"

	"github.com/google/gopacket/layers"
	"github.com/safing/portbase/log"
	"github.com/safing/portbase/rng"
	"github.com/safing/portmaster/intel/geoip"
	"github.com/safing/portmaster/network/netutils"
	"github.com/safing/portmaster/network/packet"
)

var (
	locationTestingIPv4     = "1.1.1.1"
	locationTestingIPv4Addr *net.IPAddr

	locations = &DeviceLocations{
		All: make(map[string]*DeviceLocation),
	}
	locationsLock              sync.Mutex
	gettingLocationsLock       sync.Mutex
	locationNetworkChangedFlag = GetNetworkChangedFlag()
)

func prepLocation() (err error) {
	locationTestingIPv4Addr, err = net.ResolveIPAddr("ip", locationTestingIPv4)
	return err
}

type DeviceLocations struct {
	Best *DeviceLocation
	All  map[string]*DeviceLocation
}

func copyDeviceLocations() *DeviceLocations {
	locationsLock.Lock()
	defer locationsLock.Unlock()

	// Create a copy of the locations, but not the entries.
	cp := *locations
	cp.All = make(map[string]*DeviceLocation, len(locations.All))
	for k, v := range locations.All {
		cp.All[k] = v
	}

	return &cp
}

// DeviceLocation represents a single IP and metadata. It must not be changed
// once created.
type DeviceLocation struct {
	IP             net.IP
	Continent      string
	Country        string
	ASN            uint
	ASOrg          string
	Source         DeviceLocationSource
	SourceAccuracy int
}

type DeviceLocationSource string

const (
	SourceInterface  DeviceLocationSource = "interface"
	SourcePeer       DeviceLocationSource = "peer"
	SourceUPNP       DeviceLocationSource = "upnp"
	SourceTraceroute DeviceLocationSource = "traceroute"
	SourceOther      DeviceLocationSource = "other"
)

func (dls DeviceLocationSource) Accuracy() int {
	switch dls {
	case SourceInterface:
		return 5
	case SourcePeer:
		return 4
	case SourceUPNP:
		return 3
	case SourceTraceroute:
		return 2
	case SourceOther:
		return 1
	default:
		return 0
	}
}

func SetInternetLocation(ip net.IP, source DeviceLocationSource) (ok bool) {
	// Check if IP is global.
	if netutils.GetIPScope(ip) != netutils.Global {
		return false
	}

	// Create new location.
	loc := &DeviceLocation{
		IP:             ip,
		Source:         source,
		SourceAccuracy: source.Accuracy(),
	}

	locationsLock.Lock()
	defer locationsLock.Unlock()

	// Add to locations, if better.
	key := loc.IP.String()
	existing, ok := locations.All[key]
	if ok && existing.SourceAccuracy > loc.SourceAccuracy {
		// Existing entry is better.
		// Return true, because the IP address is part of the locations.
		return true
	}
	locations.All[key] = loc

	// Find best location.
	var best *DeviceLocation
	for _, dl := range locations.All {
		if best == nil || dl.SourceAccuracy > best.SourceAccuracy {
			best = dl
		}
	}
	locations.Best = best

	// Get geoip information, but continue if it fails.
	geoLoc, err := geoip.GetLocation(ip)
	if err != nil {
		log.Warningf("netenv: failed to get geolocation data of %s (from %s): %s", ip, source, err)
	} else {
		loc.Continent = geoLoc.Continent.Code
		loc.Country = geoLoc.Country.ISOCode
		loc.ASN = geoLoc.AutonomousSystemNumber
		loc.ASOrg = geoLoc.AutonomousSystemOrganization
	}

	return true
}

// DEPRECATED: Please use GetInternetLocation instead.
func GetApproximateInternetLocation() (net.IP, error) {
	loc, ok := GetInternetLocation()
	if !ok {
		return nil, errors.New("no location data available")
	}
	return loc.Best.IP, nil
}

func GetInternetLocation() (deviceLocations *DeviceLocations, ok bool) {
	gettingLocationsLock.Lock()
	defer gettingLocationsLock.Unlock()

	// Check if the network changed, if not, return cache.
	if !locationNetworkChangedFlag.IsSet() {
		return copyDeviceLocations(), true
	}
	locationNetworkChangedFlag.Refresh()

	// Check different sources, return on first success.
	switch {
	case getLocationFromInterfaces():
	case getLocationFromTraceroute():
	default:
		return nil, false
	}

	// Return gathered locations.
	cp := copyDeviceLocations()
	return cp, cp.Best != nil
}

func getLocationFromInterfaces() (ok bool) {
	globalIPv4, globalIPv6, err := GetAssignedGlobalAddresses()
	if err != nil {
		log.Warningf("netenv: location: failed to get assigned global addresses: %s", err)
		return false
	}

	for _, ip := range globalIPv4 {
		if SetInternetLocation(ip, SourceInterface) {
			ok = true
		}
	}
	for _, ip := range globalIPv6 {
		if SetInternetLocation(ip, SourceInterface) {
			ok = true
		}
	}

	return ok
}

// TODO: Check feasibility of getting the external IP via UPnP.
/*
func getLocationFromUPnP() (ok bool) {
	// Endoint: urn:schemas-upnp-org:service:WANIPConnection:1#GetExternalIPAddress
	// A first test showed that a router did offer that endpoint, but did not
	// return an IP addres.
	return false
}
*/

func getLocationFromTraceroute() (ok bool) {
	// Create connection.
	conn, err := net.ListenPacket("ip4:icmp", "")
	if err != nil {
		log.Warningf("netenv: location: failed to open icmp conn: %s", err)
		return false
	}
	v4Conn := ipv4.NewPacketConn(conn)

	// Generate a random ID for the ICMP packets.
	generatedID, err := rng.Number(0xFFFF) // uint16
	if err != nil {
		log.Warningf("netenv: location: failed to generate icmp msg ID: %s", err)
		return false
	}
	msgID := int(generatedID)
	var msgSeq int

	// Create ICMP message body.
	pingMessage := icmp.Message{
		Type: ipv4.ICMPTypeEcho,
		Code: 0,
		Body: &icmp.Echo{
			ID:   msgID,
			Seq:  msgSeq, // Is increased before marshalling.
			Data: []byte{},
		},
	}
	maxHops := 4 // add one for every reply that is not global

	// Get additional listener for ICMP messages via the firewall.
	icmpPacketsViaFirewall, doneWithListeningToICMP := ListenToICMP()
	defer doneWithListeningToICMP()

nextHop:
	for i := 1; i <= maxHops; i++ {
		minSeq := msgSeq + 1

	repeat:
		for j := 1; j <= 2; j++ { // Try every hop twice.
			// Increase sequence number.
			msgSeq++
			pingMessage.Body.(*icmp.Echo).Seq = msgSeq

			// Make packet data.
			pingPacket, err := pingMessage.Marshal(nil)
			if err != nil {
				log.Warningf("netenv: location: failed to build icmp packet: %s", err)
				return false
			}

			// Set TTL on IP packet.
			err = v4Conn.SetTTL(i)
			if err != nil {
				log.Warningf("netenv: location: failed to set icmp packet TTL: %s", err)
				return false
			}

			// Send ICMP packet.
			if _, err := conn.WriteTo(pingPacket, locationTestingIPv4Addr); err != nil {
				if neterr, ok := err.(*net.OpError); ok {
					if neterr.Err == syscall.ENOBUFS {
						continue
					}
				}
				log.Warningf("netenv: location: failed to send icmp packet: %s", err)
				return false
			}

			// Listen for replies of the ICMP packet.
		listen:
			for {
				remoteIP, icmpPacket, ok := recvICMP(i, icmpPacketsViaFirewall)
				if !ok {
					continue repeat
				}

				// Pre-filter by message type.
				switch icmpPacket.TypeCode.Type() {
				case layers.ICMPv4TypeEchoReply:
					// Check if the ID and sequence match.
					if icmpPacket.Id != uint16(msgID) {
						continue listen
					}
					if icmpPacket.Seq < uint16(minSeq) {
						continue listen
					}
					// We received a reply, so we did not trigger a time exceeded response on the way.
					// This means we were not able to find the nearest router to us.
					return false
				case layers.ICMPv4TypeDestinationUnreachable,
					layers.ICMPv4TypeTimeExceeded:
					// Continue processing.
				default:
					continue listen
				}

				// Parse copy of origin icmp packet that triggered the error.
				if len(icmpPacket.Payload) != ipv4.HeaderLen+8 {
					continue listen
				}
				originalMessage, err := icmp.ParseMessage(1, icmpPacket.Payload[ipv4.HeaderLen:])
				if err != nil {
					continue listen
				}
				originalEcho, ok := originalMessage.Body.(*icmp.Echo)
				if !ok {
					continue listen
				}
				// Check if the ID and sequence match.
				if originalEcho.ID != int(msgID) {
					continue listen
				}
				if originalEcho.Seq < minSeq {
					continue listen
				}

				// React based on message type.
				switch icmpPacket.TypeCode.Type() {
				case layers.ICMPv4TypeDestinationUnreachable:
					// We have received a valid destination unreachable response, abort.
					return false

				case layers.ICMPv4TypeTimeExceeded:
					// We have received a valid time exceeded error.
					// If message came from a global unicast, us it!
					if netutils.GetIPScope(remoteIP) == netutils.Global {
						return SetInternetLocation(remoteIP, SourceTraceroute)
					}

					// Otherwise, continue.
					continue nextHop
				}
			}
		}
	}

	// We did not receive anything actionable.
	return false
}

func recvICMP(currentHop int, icmpPacketsViaFirewall chan packet.Packet) (
	remoteIP net.IP, imcpPacket *layers.ICMPv4, ok bool) {

	for {
		select {
		case pkt := <-icmpPacketsViaFirewall:
			if pkt.IsOutbound() {
				continue
			}
			if pkt.Layers() == nil {
				continue
			}
			icmpLayer := pkt.Layers().Layer(layers.LayerTypeICMPv4)
			if icmpLayer == nil {
				continue
			}
			icmp4, ok := icmpLayer.(*layers.ICMPv4)
			if !ok {
				continue
			}
			return pkt.Info().RemoteIP(), icmp4, true

		case <-time.After(time.Duration(currentHop*10+50) * time.Millisecond):
			return nil, nil, false
		}
	}
}
