package packet

import (
	"errors"
	"fmt"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
)

var LayerType2IPProtocol map[gopacket.LayerType]IPProtocol

func genIPProtocolFromLayerType() {
	LayerType2IPProtocol = make(map[gopacket.LayerType]IPProtocol)
	for k, v := range layers.IPProtocolMetadata {
		LayerType2IPProtocol[v.LayerType] = IPProtocol(k)
	}
}

// Parse parses an IP packet and saves the information in the given packet object.
func Parse(packetData []byte, pktInfo *Info) error {

	var parsedPacket gopacket.Packet

	if len(packetData) == 0 {
		return errors.New("empty packet")
	}

	switch packetData[0] >> 4 {
	case 4:
		parsedPacket = gopacket.NewPacket(packetData, layers.LayerTypeIPv4, gopacket.DecodeOptions{Lazy: true, NoCopy: true})
		if ipv4Layer := parsedPacket.Layer(layers.LayerTypeIPv4); ipv4Layer != nil {
			ipv4, _ := ipv4Layer.(*layers.IPv4)
			pktInfo.Version = IPv4
			pktInfo.Protocol = IPProtocol(ipv4.Protocol)
			pktInfo.Src = ipv4.SrcIP
			pktInfo.Dst = ipv4.DstIP
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
			pktInfo.Version = IPv6
			pktInfo.Protocol = LayerType2IPProtocol[ipv6.NextLayerType()]
			pktInfo.Src = ipv6.SrcIP
			pktInfo.Dst = ipv6.DstIP
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

	switch pktInfo.Protocol {
	case TCP:
		if tcpLayer := parsedPacket.Layer(layers.LayerTypeTCP); tcpLayer != nil {
			tcp, _ := tcpLayer.(*layers.TCP)
			pktInfo.SrcPort = uint16(tcp.SrcPort)
			pktInfo.DstPort = uint16(tcp.DstPort)
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
			pktInfo.SrcPort = uint16(udp.SrcPort)
			pktInfo.DstPort = uint16(udp.DstPort)
		} else {
			var err error
			if errLayer := parsedPacket.ErrorLayer(); errLayer != nil {
				err = errLayer.Error()
			}
			return fmt.Errorf("could not parse UDP layer: %s", err)
		}
	}

	if errLayer := parsedPacket.ErrorLayer(); errLayer != nil {
		return errLayer.Error()
	}

	return nil
}

func init() {
	genIPProtocolFromLayerType()
}
