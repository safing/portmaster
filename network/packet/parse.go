package packet

import (
	"errors"
	"fmt"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"github.com/safing/portbase/log"
)

var layerType2IPProtocol map[gopacket.LayerType]IPProtocol

func genIPProtocolFromLayerType() {
	layerType2IPProtocol = make(map[gopacket.LayerType]IPProtocol)
	for k, v := range layers.IPProtocolMetadata {
		layerType2IPProtocol[v.LayerType] = IPProtocol(k)
	}
}

func parseIPv4(packet gopacket.Packet, info *Info) error {
	if ipv4, ok := packet.NetworkLayer().(*layers.IPv4); ok {
		info.Version = IPv4
		info.Src = ipv4.SrcIP
		info.Dst = ipv4.DstIP
		info.Protocol = IPProtocol(ipv4.Protocol)
	}
	return nil
}

func parseIPv6(packet gopacket.Packet, info *Info) error {
	if ipv6, ok := packet.NetworkLayer().(*layers.IPv6); ok {
		info.Version = IPv6
		info.Src = ipv6.SrcIP
		info.Dst = ipv6.DstIP
	}
	return nil
}

func parseTCP(packet gopacket.Packet, info *Info) error {
	if tcp, ok := packet.TransportLayer().(*layers.TCP); ok {
		info.Protocol = TCP
		info.SrcPort = uint16(tcp.SrcPort)
		info.DstPort = uint16(tcp.DstPort)
	}
	return nil
}

func parseUDP(packet gopacket.Packet, info *Info) error {
	if udp, ok := packet.TransportLayer().(*layers.UDP); ok {
		info.Protocol = UDP
		info.SrcPort = uint16(udp.SrcPort)
		info.DstPort = uint16(udp.DstPort)
	}
	return nil
}

func parseUDPLite(packet gopacket.Packet, info *Info) error {
	if udpLite, ok := packet.TransportLayer().(*layers.UDPLite); ok {
		info.Protocol = UDPLite
		info.SrcPort = uint16(udpLite.SrcPort)
		info.DstPort = uint16(udpLite.DstPort)
	}
	return nil
}

func parseICMPv4(packet gopacket.Packet, info *Info) error {
	if icmp, ok := packet.Layer(layers.LayerTypeICMPv4).(*layers.ICMPv4); ok {
		info.Protocol = ICMP
		_ = icmp
	}
	return nil
}

func parseICMPv6(packet gopacket.Packet, info *Info) error {
	if icmp6, ok := packet.Layer(layers.LayerTypeICMPv6).(*layers.ICMPv6); ok {
		info.Protocol = ICMPv6
		_ = icmp6
	}
	return nil
}

func parseIGMP(packet gopacket.Packet, info *Info) error {
	// gopacket uses LayerTypeIGMP for v1, v2 and v3 and may thus
	// either return layers.IGMP or layers.IGMPv1or2
	if layer := packet.Layer(layers.LayerTypeIGMP); layer != nil {
		info.Protocol = IGMP
	}
	return nil
}

func checkError(packet gopacket.Packet, _ *Info) error {
	if err := packet.ErrorLayer(); err != nil {
		return err.Error()
	}
	return nil
}

func tryFindIPv6TransportProtocol(packet gopacket.Packet, info *Info) {
	if transport := packet.TransportLayer(); transport != nil {
		proto, ok := layerType2IPProtocol[transport.LayerType()]

		if ok {
			info.Protocol = proto
			log.Tracef("packet: unsupported IPv6 protocol %02x (%d)", proto)
		} else {
			log.Warningf("packet: unsupported or unknown gopacket layer type: %d", transport.LayerType())
		}
		return
	}
	log.Tracef("packet: failed to get IPv6 transport protocol number")
}

// Parse parses an IP packet and saves the information in the given packet object.
func Parse(packetData []byte, pktInfo *Info) error {
	if len(packetData) == 0 {
		return errors.New("empty packet")
	}

	ipVersion := packetData[0] >> 4
	var networkLayerType gopacket.LayerType

	switch ipVersion {
	case 4:
		networkLayerType = layers.LayerTypeIPv4
	case 6:
		networkLayerType = layers.LayerTypeIPv6
	default:
		return fmt.Errorf("unknown IP version or network protocol: %02x", ipVersion)
	}

	// 255 is reserved by IANA so we use it as a "failed-to-detect" marker.
	pktInfo.Protocol = 255

	packet := gopacket.NewPacket(packetData, networkLayerType, gopacket.DecodeOptions{
		Lazy:   true,
		NoCopy: true,
	})

	availableDecoders := []func(gopacket.Packet, *Info) error{
		parseIPv4,
		parseIPv6,
		parseTCP,
		parseUDP,
		//parseUDPLite, // we don't yet support udplite
		parseICMPv4,
		parseICMPv6,
		parseIGMP,
		checkError,
	}

	for _, dec := range availableDecoders {
		if err := dec(packet, pktInfo); err != nil {
			return err
		}
	}

	// 255 is reserved by IANA and used as a "failed-to-detect"
	// marker for IPv6 (parseIPv4 always sets the protocl field)
	if pktInfo.Protocol == 255 {
		tryFindIPv6TransportProtocol(packet, pktInfo)
	}

	return nil
}

func init() {
	genIPProtocolFromLayerType()
}
