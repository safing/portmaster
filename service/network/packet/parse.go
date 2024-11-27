package packet

import (
	"errors"
	"fmt"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
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
		// we set Protocol to NextHeader as a fallback. If TCP or
		// UDP layers are detected (somewhere in the list of options)
		// the Protocol field is adjusted correctly.
		info.Protocol = IPProtocol(ipv6.NextHeader)
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

/*
func parseUDPLite(packet gopacket.Packet, info *Info) error {
	if udpLite, ok := packet.TransportLayer().(*layers.UDPLite); ok {
		info.Protocol = UDPLite
		info.SrcPort = uint16(udpLite.SrcPort)
		info.DstPort = uint16(udpLite.DstPort)
	}
	return nil
}
*/

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

func checkError(packet gopacket.Packet, info *Info) error {
	// Check for known unparseable before checking the error layer.
	if info.Protocol == AnyHostInternalProtocol61 {
		return nil
	}

	if err := packet.ErrorLayer(); err != nil {
		return err.Error()
	}
	return nil
}

// ParseLayer3 parses an IP packet and saves the information in the given packet object.
func ParseLayer3(packetData []byte, pktBase *Base) (err error) {
	if len(packetData) == 0 {
		return errors.New("empty packet")
	}

	pktBase.layer3Data = packetData

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

	packet := gopacket.NewPacket(packetData, networkLayerType, gopacket.DecodeOptions{
		Lazy:   true,
		NoCopy: true,
	})

	availableDecoders := []func(gopacket.Packet, *Info) error{
		parseIPv4,
		parseIPv6,
		parseTCP,
		parseUDP,
		// parseUDPLite, // We don't yet support udplite.
		parseICMPv4,
		parseICMPv6,
		parseIGMP,
		checkError,
	}

	for _, dec := range availableDecoders {
		if err := dec(packet, pktBase.Info()); err != nil {
			return err
		}
	}

	pktBase.layers = packet
	if transport := packet.TransportLayer(); transport != nil {
		pktBase.layer5Data = transport.LayerPayload()
	}
	return nil
}

// ParseLayer4 parses an layer 4 packet and saves the information in the given packet object.
func ParseLayer4(packetData []byte, pktBase *Base) (err error) {
	if len(packetData) == 0 {
		return errors.New("empty packet")
	}

	var layer gopacket.LayerType
	switch pktBase.info.Protocol {
	case ICMP:
		layer = layers.LayerTypeICMPv4
	case IGMP:
		layer = layers.LayerTypeIGMP
	case TCP:
		layer = layers.LayerTypeTCP
	case UDP:
		layer = layers.LayerTypeUDP
	case ICMPv6:
		layer = layers.LayerTypeICMPv6
	case UDPLite:
		return fmt.Errorf("UDPLite not supported")
	case RAW:
		return fmt.Errorf("RAW protocol not supported")
	case AnyHostInternalProtocol61:
		return fmt.Errorf("AnyHostInternalProtocol61 protocol not supported")
	default:
		return fmt.Errorf("protocol not supported")
	}

	packet := gopacket.NewPacket(packetData, layer, gopacket.DecodeOptions{
		Lazy:   true,
		NoCopy: true,
	})

	availableDecoders := []func(gopacket.Packet, *Info) error{
		parseTCP,
		parseUDP,
		// parseUDPLite, // We don't yet support udplite.
		parseICMPv4,
		parseICMPv6,
		parseIGMP,
		checkError,
	}

	for _, dec := range availableDecoders {
		if err := dec(packet, pktBase.Info()); err != nil {
			return err
		}
	}

	pktBase.layers = packet
	if transport := packet.TransportLayer(); transport != nil {
		pktBase.layer5Data = transport.LayerPayload()
	}
	return nil
}

func init() {
	genIPProtocolFromLayerType()
}
