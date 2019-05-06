package packet

import (
	"errors"
	"fmt"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
)

// Parse parses an IP packet and saves the information in the given packet object.
func Parse(packetData []byte, packet *Base) error {

	var parsedPacket gopacket.Packet

	if len(packetData) == 0 {
		return errors.New("empty packet")
	}

	switch packetData[0] >> 4 {
	case 4:
		parsedPacket = gopacket.NewPacket(packetData, layers.LayerTypeIPv4, gopacket.DecodeOptions{Lazy: true, NoCopy: true})
		if ipv4Layer := parsedPacket.Layer(layers.LayerTypeIPv4); ipv4Layer != nil {
			ipv4, _ := ipv4Layer.(*layers.IPv4)
			packet.info.Version = IPv4
			packet.info.Protocol = IPProtocol(ipv4.Protocol)
			packet.info.Src = ipv4.SrcIP
			packet.info.Dst = ipv4.DstIP
		} else {
			var err error
			if errLayer := parsedPacket.ErrorLayer(); errLayer != nil {
				err = errLayer.Error()
			}
			return fmt.Errorf("failed to parse IPv4 packet: %s", err)
		}
	case 6:
		parsedPacket = gopacket.NewPacket(packetData, layers.LayerTypeIPv6, gopacket.DecodeOptions{Lazy: true, NoCopy: true})
		if ipv6Layer := parsedPacket.Layer(layers.LayerTypeIPv6); ipv6Layer != nil {
			ipv6, _ := ipv6Layer.(*layers.IPv6)
			packet.info.Version = IPv6
			packet.info.Protocol = IPProtocol(ipv6.NextHeader)
			packet.info.Src = ipv6.SrcIP
			packet.info.Dst = ipv6.DstIP
		} else {
			var err error
			if errLayer := parsedPacket.ErrorLayer(); errLayer != nil {
				err = errLayer.Error()
			}
			return fmt.Errorf("failed to parse IPv6 packet: %s", err)
		}
	default:
		return errors.New("unknown IP version")
	}

	switch packet.info.Protocol {
	case TCP:
		if tcpLayer := parsedPacket.Layer(layers.LayerTypeTCP); tcpLayer != nil {
			tcp, _ := tcpLayer.(*layers.TCP)
			packet.info.SrcPort = uint16(tcp.SrcPort)
			packet.info.DstPort = uint16(tcp.DstPort)
		} else {
			var err error
			if errLayer := parsedPacket.ErrorLayer(); errLayer != nil {
				err = errLayer.Error()
			}
			return fmt.Errorf("could not parse TCP layer: %s", err)
		}
	case UDP:
		if udpLayer := parsedPacket.Layer(layers.LayerTypeUDP); udpLayer != nil {
			udp, _ := udpLayer.(*layers.UDP)
			packet.info.SrcPort = uint16(udp.SrcPort)
			packet.info.DstPort = uint16(udp.DstPort)
		} else {
			var err error
			if errLayer := parsedPacket.ErrorLayer(); errLayer != nil {
				err = errLayer.Error()
			}
			return fmt.Errorf("could not parse UDP layer: %s", err)
		}
	}

	if appLayer := parsedPacket.ApplicationLayer(); appLayer != nil {
		packet.Payload = appLayer.Payload()
	}

	if errLayer := parsedPacket.ErrorLayer(); errLayer != nil {
		return errLayer.Error()
	}

	return nil
}
