package netenv

import (
	"errors"
	"fmt"
	"net"
	"sort"
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
	// locationTestingIPv4 holds the IP address of the server that should be
	// tracerouted to find the location of the device. The ping will never reach
	// the destination in most cases.
	// The selection of this IP requires sensitivity, as the IP address must be
	// far enough away to produce good results.
	// At the same time, the IP address should be common and not raise attention.
	locationTestingIPv4     = "1.1.1.1"
	locationTestingIPv4Addr *net.IPAddr

	locations                  = &DeviceLocations{}
	locationsLock              sync.Mutex
	gettingLocationsLock       sync.Mutex
	locationNetworkChangedFlag = GetNetworkChangedFlag()
)

func prepLocation() (err error) {
	locationTestingIPv4Addr, err = net.ResolveIPAddr("ip", locationTestingIPv4)
	return err
}

type DeviceLocations struct {
	All []*DeviceLocation
}

func (dl *DeviceLocations) Best() *DeviceLocation {
	if len(dl.All) > 0 {
		return dl.All[0]
	}
	return nil
}

func (dl *DeviceLocations) BestV4() *DeviceLocation {
	for _, loc := range dl.All {
		if loc.IPVersion == packet.IPv4 {
			return loc
		}
	}
	return nil
}

func (dl *DeviceLocations) BestV6() *DeviceLocation {
	for _, loc := range dl.All {
		if loc.IPVersion == packet.IPv6 {
			return loc
		}
	}
	return nil
}

func copyDeviceLocations() *DeviceLocations {
	locationsLock.Lock()
	defer locationsLock.Unlock()

	// Create a copy of the locations, but not the entries.
	cp := &DeviceLocations{
		All: make([]*DeviceLocation, len(locations.All)),
	}
	copy(cp.All, locations.All)

	return cp
}

// DeviceLocation represents a single IP and metadata. It must not be changed
// once created.
type DeviceLocation struct {
	IP             net.IP
	IPVersion      packet.IPVersion
	Location       *geoip.Location
	Source         DeviceLocationSource
	SourceAccuracy int
}

// IsMoreAccurateThan checks if the device location is more accurate than the
// given one.
func (dl *DeviceLocation) IsMoreAccurateThan(other *DeviceLocation) bool {
	switch {
	case dl.SourceAccuracy > other.SourceAccuracy:
		// Higher source accuracy is better.
		return true
	case dl.IP != nil && other.IP == nil:
		// Location based on IP is better than without.
		return true
	case dl.Location.AutonomousSystemNumber != 0 &&
		other.Location.AutonomousSystemNumber == 0:
		// Having an ASN is better than having none.
		return true
	case dl.Location.Continent.Code != "" &&
		other.Location.Continent.Code == "":
		// Having a Continent is better than having none.
		return true
	case dl.Location.Country.ISOCode != "" &&
		other.Location.Country.ISOCode == "":
		// Having a Country is better than having none.
		return true
	case (dl.Location.Coordinates.Latitude != 0 ||
		dl.Location.Coordinates.Longitude != 0) &&
		other.Location.Coordinates.Latitude == 0 &&
		other.Location.Coordinates.Longitude == 0:
		// Having Coordinates is better than having none.
	case dl.Location.Coordinates.AccuracyRadius < other.Location.Coordinates.AccuracyRadius:
		// Higher geo accuracy is better.
		return true
	}

	return false
}

func (dl *DeviceLocation) LocationOrNil() *geoip.Location {
	if dl == nil {
		return nil
	}
	return dl.Location
}

func (dl *DeviceLocation) String() string {
	switch {
	case dl == nil:
		return "<none>"
	case dl.Location == nil:
		return dl.IP.String()
	default:
		return fmt.Sprintf("%s (AS%d in %s)", dl.IP, dl.Location.AutonomousSystemNumber, dl.Location.Country.ISOCode)
	}
}

type DeviceLocationSource string

const (
	SourceInterface  DeviceLocationSource = "interface"
	SourcePeer       DeviceLocationSource = "peer"
	SourceUPNP       DeviceLocationSource = "upnp"
	SourceTraceroute DeviceLocationSource = "traceroute"
	SourceTimezone   DeviceLocationSource = "timezone"
	SourceOther      DeviceLocationSource = "other"
)

func (dls DeviceLocationSource) Accuracy() int {
	switch dls {
	case SourceInterface:
		return 6
	case SourcePeer:
		return 5
	case SourceUPNP:
		return 4
	case SourceTraceroute:
		return 3
	case SourceOther:
		return 2
	case SourceTimezone:
		return 1
	default:
		return 0
	}
}

type sortLocationsByAccuracy []*DeviceLocation

func (a sortLocationsByAccuracy) Len() int           { return len(a) }
func (a sortLocationsByAccuracy) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a sortLocationsByAccuracy) Less(i, j int) bool { return !a[j].IsMoreAccurateThan(a[i]) }

func SetInternetLocation(ip net.IP, source DeviceLocationSource) (dl *DeviceLocation, ok bool) {
	// Check if IP is global.
	if netutils.GetIPScope(ip) != netutils.Global {
		return nil, false
	}

	// Create new location.
	loc := &DeviceLocation{
		IP:             ip,
		Source:         source,
		SourceAccuracy: source.Accuracy(),
	}
	if v4 := ip.To4(); v4 != nil {
		loc.IPVersion = packet.IPv4
	} else {
		loc.IPVersion = packet.IPv6
	}

	// Get geoip information, but continue if it fails.
	geoLoc, err := geoip.GetLocation(ip)
	if err != nil {
		log.Warningf("netenv: failed to get geolocation data of %s (from %s): %s", ip, source, err)
	} else {
		loc.Location = geoLoc
	}

	addLocation(loc)
	return loc, true
}

func addLocation(dl *DeviceLocation) {
	locationsLock.Lock()
	defer locationsLock.Unlock()

	// Add to locations, if better.
	var exists bool
	for i, existing := range locations.All {
		if (dl.IP == nil && existing.IP == nil) || dl.IP.Equal(existing.IP) {
			exists = true
			if dl.IsMoreAccurateThan(existing) {
				// Replace
				locations.All[i] = dl
				break
			}
		}
	}
	if !exists {
		locations.All = append(locations.All, dl)
	}

	// Sort locations.
	sort.Sort(sortLocationsByAccuracy(locations.All))
}

// DEPRECATED: Please use GetInternetLocation instead.
func GetApproximateInternetLocation() (net.IP, error) {
	loc, ok := GetInternetLocation()
	if !ok || loc.Best() == nil {
		return nil, errors.New("no location data available")
	}
	return loc.Best().IP, nil
}

func GetInternetLocation() (deviceLocations *DeviceLocations, ok bool) {
	gettingLocationsLock.Lock()
	defer gettingLocationsLock.Unlock()

	// Check if the network changed, if not, return cache.
	if !locationNetworkChangedFlag.IsSet() {
		return copyDeviceLocations(), true
	}
	locationNetworkChangedFlag.Refresh()

	// Get all assigned addresses.
	v4s, v6s, err := GetAssignedAddresses()
	if err != nil {
		log.Warningf("netenv: failed to get assigned addresses: %s", err)
		return nil, false
	}

	// Check interfaces for global addresses.
	v4ok, v6ok := getLocationFromInterfaces()

	// Try other methods for missing locations.
	if len(v4s) > 0 {
		if !v4ok {
			_, err = getLocationFromTraceroute()
			if err != nil {
				log.Warningf("netenv: failed to get IPv4 from traceroute: %s", err)
			} else {
				v4ok = true
			}
		}
		if !v4ok {
			v4ok = getLocationFromTimezone(packet.IPv4)
		}
	}
	if len(v6s) > 0 && !v6ok {
		// TODO
		log.Warningf("netenv: could not get IPv6 location")
	}

	// Check if we have any locations.
	if !v4ok && !v6ok {
		return nil, false
	}

	// Return gathered locations.
	cp := copyDeviceLocations()
	return cp, true
}

func getLocationFromInterfaces() (v4ok, v6ok bool) {
	globalIPv4, globalIPv6, err := GetAssignedGlobalAddresses()
	if err != nil {
		log.Warningf("netenv: location: failed to get assigned global addresses: %s", err)
		return false, false
	}

	for _, ip := range globalIPv4 {
		if _, ok := SetInternetLocation(ip, SourceInterface); ok {
			v4ok = true
		}
	}
	for _, ip := range globalIPv6 {
		if _, ok := SetInternetLocation(ip, SourceInterface); ok {
			v6ok = true
		}
	}

	return
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

func getLocationFromTraceroute() (dl *DeviceLocation, err error) {
	// Create connection.
	conn, err := net.ListenPacket("ip4:icmp", "")
	if err != nil {
		return nil, fmt.Errorf("failed to open icmp conn: %s", err)
	}
	v4Conn := ipv4.NewPacketConn(conn)

	// Generate a random ID for the ICMP packets.
	generatedID, err := rng.Number(0xFFFF) // uint16
	if err != nil {
		return nil, fmt.Errorf("failed to generate icmp msg ID: %s", err)
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
	icmpPacketsViaFirewall, doneWithListeningToICMP := ListenToICMP(locationTestingIPv4Addr.IP)
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
				return nil, fmt.Errorf("failed to build icmp packet: %s", err)
			}

			// Set TTL on IP packet.
			err = v4Conn.SetTTL(i)
			if err != nil {
				return nil, fmt.Errorf("failed to set icmp packet TTL: %s", err)
			}

			// Send ICMP packet.
			if _, err := conn.WriteTo(pingPacket, locationTestingIPv4Addr); err != nil {
				if neterr, ok := err.(*net.OpError); ok {
					if neterr.Err == syscall.ENOBUFS {
						continue
					}
				}
				return nil, fmt.Errorf("failed to send icmp packet: %s", err)
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
					return nil, errors.New("received final echo reply without time exceeded messages")
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
					return nil, errors.New("destination unreachable")

				case layers.ICMPv4TypeTimeExceeded:
					// We have received a valid time exceeded error.
					// If message came from a global unicast, us it!
					if netutils.GetIPScope(remoteIP) == netutils.Global {
						dl, ok := SetInternetLocation(remoteIP, SourceTraceroute)
						if !ok {
							return nil, errors.New("invalid IP address")
						}
						return dl, nil
					}

					// Otherwise, continue.
					continue nextHop
				}
			}
		}
	}

	// We did not receive anything actionable.
	return nil, errors.New("did not receive any actionable ICMP reply")
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

		case <-time.After(time.Duration(currentHop*20+100) * time.Millisecond):
			return nil, nil, false
		}
	}
}

func getLocationFromTimezone(ipVersion packet.IPVersion) (ok bool) {
	// Create base struct.
	tzLoc := &DeviceLocation{
		IPVersion:      ipVersion,
		Location:       &geoip.Location{},
		Source:         SourceTimezone,
		SourceAccuracy: SourceTimezone.Accuracy(),
	}

	// Calculate longitude based on current timezone.
	_, offsetSeconds := time.Now().Zone()
	tzLoc.Location.Coordinates.AccuracyRadius = 1000
	tzLoc.Location.Coordinates.Latitude = 48
	tzLoc.Location.Coordinates.Longitude = float64(offsetSeconds) / 43200 * 180

	addLocation(tzLoc)
	return true
}
