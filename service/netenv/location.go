package netenv

import (
	"errors"
	"fmt"
	"net"
	"sort"
	"sync"
	"time"

	"github.com/google/gopacket/layers"
	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"

	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/base/rng"
	"github.com/safing/portmaster/service/intel/geoip"
	"github.com/safing/portmaster/service/network/netutils"
	"github.com/safing/portmaster/service/network/packet"
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

// DeviceLocations holds multiple device locations.
type DeviceLocations struct {
	All []*DeviceLocation
}

// Best returns the best (most accurate) device location.
func (dls *DeviceLocations) Best() *DeviceLocation {
	if len(dls.All) > 0 {
		return dls.All[0]
	}
	return nil
}

// BestV4 returns the best (most accurate) IPv4 device location.
func (dls *DeviceLocations) BestV4() *DeviceLocation {
	for _, loc := range dls.All {
		if loc.IPVersion == packet.IPv4 {
			return loc
		}
	}
	return nil
}

// BestV6 returns the best (most accurate) IPv6 device location.
func (dls *DeviceLocations) BestV6() *DeviceLocation {
	for _, loc := range dls.All {
		if loc.IPVersion == packet.IPv6 {
			return loc
		}
	}
	return nil
}

// Copy creates a copy of the locations, but not the individual entries.
func (dls *DeviceLocations) Copy() *DeviceLocations {
	cp := &DeviceLocations{
		All: make([]*DeviceLocation, len(locations.All)),
	}
	copy(cp.All, locations.All)

	return cp
}

// AddLocation adds a location.
func (dls *DeviceLocations) AddLocation(dl *DeviceLocation) {
	if dls == nil {
		return
	}

	// Add to locations, if better.
	var exists bool
	for i, existing := range dls.All {
		if (dl.IP == nil && existing.IP == nil) || dl.IP.Equal(existing.IP) {
			exists = true
			if dl.IsMoreAccurateThan(existing) {
				// Replace
				dls.All[i] = dl
				break
			}
		}
	}
	if !exists {
		dls.All = append(dls.All, dl)
	}

	// Sort locations.
	sort.Sort(sortLocationsByAccuracy(dls.All))

	log.Debugf("netenv: added new device location to IPv%d scope: %s from %s", dl.IPVersion, dl, dl.Source)
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
	case dl.Location.Country.Code != "" &&
		other.Location.Country.Code == "":
		// Having a Country is better than having none.
		return true
	case (dl.Location.Coordinates.Latitude != 0 ||
		dl.Location.Coordinates.Longitude != 0) &&
		other.Location.Coordinates.Latitude == 0 &&
		other.Location.Coordinates.Longitude == 0:
		// Having Coordinates is better than having none.
		return true
	case dl.Location.Coordinates.AccuracyRadius < other.Location.Coordinates.AccuracyRadius:
		// Higher geo accuracy is better.
		return true
	}

	return false
}

// LocationOrNil or returns the geoip location, or nil if not present.
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
	case dl.Source == SourceTimezone:
		return fmt.Sprintf(
			"TZ(%.0f/%.0f)",
			dl.Location.Coordinates.Latitude,
			dl.Location.Coordinates.Longitude,
		)
	default:
		return fmt.Sprintf(
			"%s (AS%d in %s - %s)",
			dl.IP,
			dl.Location.AutonomousSystemNumber,
			dl.Location.Country.Name,
			dl.Location.Country.Code,
		)
	}
}

// DeviceLocationSource is a location source.
type DeviceLocationSource string

// Location Sources.
const (
	SourceInterface  DeviceLocationSource = "interface"
	SourcePeer       DeviceLocationSource = "peer"
	SourceUPNP       DeviceLocationSource = "upnp"
	SourceTraceroute DeviceLocationSource = "traceroute"
	SourceTimezone   DeviceLocationSource = "timezone"
	SourceOther      DeviceLocationSource = "other"
)

// Accuracy returns the location accuracy of the source.
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

// SetInternetLocation provides the location management system with a possible Internet location.
func SetInternetLocation(ip net.IP, source DeviceLocationSource) (dl *DeviceLocation, ok bool) {
	locationsLock.Lock()
	defer locationsLock.Unlock()

	return locations.AddIP(ip, source)
}

// AddIP adds a new location based on the given IP.
func (dls *DeviceLocations) AddIP(ip net.IP, source DeviceLocationSource) (dl *DeviceLocation, ok bool) {
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
		return nil, false
	}
	// Only use location if there is data for it.
	if geoLoc.Country.Code == "" {
		return nil, false
	}
	loc.Location = geoLoc

	dls.AddLocation(loc)
	return loc, true
}

// GetApproximateInternetLocation returns the approximate Internet location.
// Deprecated: Please use GetInternetLocation instead.
func GetApproximateInternetLocation() (net.IP, error) {
	loc, ok := GetInternetLocation()
	if !ok || loc.Best() == nil {
		return nil, errors.New("no device location data available")
	}
	return loc.Best().IP, nil
}

// GetInternetLocation returns the possible device locations.
func GetInternetLocation() (deviceLocations *DeviceLocations, ok bool) {
	gettingLocationsLock.Lock()
	defer gettingLocationsLock.Unlock()

	// Check if the network changed, if not, return cache.
	if !locationNetworkChangedFlag.IsSet() {
		locationsLock.Lock()
		defer locationsLock.Unlock()
		return locations.Copy(), true
	}
	locationNetworkChangedFlag.Refresh()

	// Create new location list.
	dls := &DeviceLocations{}
	log.Debug("netenv: getting new device locations")

	// Check interfaces for global addresses.
	v4ok, v6ok := getLocationFromInterfaces(dls)

	// Try other methods for missing locations.
	if !v4ok {
		_, err := getLocationFromTraceroute(dls)
		if err != nil {
			log.Warningf("netenv: failed to get IPv4 device location from traceroute: %s", err)
		} else {
			v4ok = true
		}

		// Get location from timezone as final fallback.
		if !v4ok {
			getLocationFromTimezone(dls, packet.IPv4)
		}
	}
	if !v6ok && IPv6Enabled() {
		// TODO: Find more ways to get IPv6 device location

		// Get location from timezone as final fallback.
		getLocationFromTimezone(dls, packet.IPv6)
	}

	// As a last guard, make sure there is at least one location in the list.
	if len(dls.All) == 0 {
		getLocationFromTimezone(dls, packet.IPv4)
	}

	// Set new locations.
	locationsLock.Lock()
	defer locationsLock.Unlock()
	locations = dls

	// Return gathered locations.
	return locations.Copy(), true
}

func getLocationFromInterfaces(dls *DeviceLocations) (v4ok, v6ok bool) {
	globalIPv4, globalIPv6, err := GetAssignedGlobalAddresses()
	if err != nil {
		log.Warningf("netenv: location: failed to get assigned global addresses: %s", err)
		return false, false
	}
	for _, ip := range globalIPv4 {
		if _, ok := dls.AddIP(ip, SourceInterface); ok {
			v4ok = true
		}
	}
	for _, ip := range globalIPv6 {
		if _, ok := dls.AddIP(ip, SourceInterface); ok {
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
	// return an IP address.
	return false
}
*/

func getLocationFromTraceroute(dls *DeviceLocations) (dl *DeviceLocation, err error) {
	// Create connection.
	conn, err := icmp.ListenPacket("ip4:icmp", "")
	if err != nil {
		return nil, fmt.Errorf("failed to open icmp conn: %w", err)
	}
	v4Conn := conn.IPv4PacketConn()

	// Generate a random ID for the ICMP packets.
	generatedID, err := rng.Number(0xFFFF) // uint16
	if err != nil {
		return nil, fmt.Errorf("failed to generate icmp msg ID: %w", err)
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

	repeatHop:
		for j := 1; j <= 2; j++ { // Try every hop twice.
			// Increase sequence number.
			msgSeq++
			pingMessage.Body.(*icmp.Echo).Seq = msgSeq //nolint:forcetypeassert // Can only be *icmp.Echo.

			// Make packet data.
			pingPacket, err := pingMessage.Marshal(nil)
			if err != nil {
				return nil, fmt.Errorf("failed to build icmp packet: %w", err)
			}

			// Set TTL on IP packet.
			err = v4Conn.SetTTL(i)
			if err != nil {
				return nil, fmt.Errorf("failed to set icmp packet TTL: %w", err)
			}

			// Send ICMP packet.
			// Try to send three times, as this can be flaky.
		sendICMP:
			for range 3 {
				_, err = conn.WriteTo(pingPacket, locationTestingIPv4Addr)
				if err == nil {
					break sendICMP
				}
				time.Sleep(30 * time.Millisecond)
			}
			if err != nil {
				return nil, fmt.Errorf("failed to send icmp packet: %w", err)
			}

			// Listen for replies of the ICMP packet.
		listen:
			for {
				remoteIP, icmpPacket, ok := recvICMP(i, icmpPacketsViaFirewall)
				if !ok {
					// Timed out.
					continue repeatHop
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
				if originalEcho.ID != msgID {
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
						dl, ok := dls.AddIP(remoteIP, SourceTraceroute)
						if !ok {
							return nil, errors.New("invalid IP address")
						}
						return dl, nil
					}

					// Add one max hop for every reply that was not global.
					maxHops++

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
	remoteIP net.IP, imcpPacket *layers.ICMPv4, ok bool,
) {
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

func getLocationFromTimezone(dls *DeviceLocations, ipVersion packet.IPVersion) {
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

	dls.AddLocation(tzLoc)
}
