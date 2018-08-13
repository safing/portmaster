package windivert

import (
	"errors"
	"fmt"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"

	"github.com/Safing/safing-core/log"
	"github.com/Safing/safing-core/network/packet"

	"github.com/tevino/abool"
)

func (wd *WinDivert) Packets(packets chan packet.Packet) error {
	go wd.packetHandler(packets)
	return nil
}

func (wd *WinDivert) packetHandler(packets chan packet.Packet) {
	defer close(packets)

	for {
		if !wd.valid.IsSet() {
			return
		}

		packetData, packetAddress, err := wd.Recv()
		if err != nil {
			log.Warningf("failed to get packet from windivert: %s", err)
			continue
		}

		ipHeader, tpcUdpHeader, payload, err := parseIpPacket(packetData)
		if err != nil {
			log.Warningf("failed to parse packet from windivert: %s", err)
			log.Warningf("failed packet payload (%d): %s", len(packetData), string(packetData))
			continue
		}

		new := &Packet{
			windivert:     wd,
			packetData:    packetData,
			packetAddress: packetAddress,
			verdictSet:    abool.NewBool(false),
		}
		new.IPHeader = ipHeader
		new.TCPUDPHeader = tpcUdpHeader
		new.Payload = payload
		if packetAddress.Direction == directionInbound {
			new.Direction = packet.InBound
		} else {
			new.Direction = packet.OutBound
		}

		packets <- new
	}
}

func parseIpPacket(packetData []byte) (ipHeader *packet.IPHeader, tpcUdpHeader *packet.TCPUDPHeader, payload []byte, err error) {

	var parsedPacket gopacket.Packet

	if len(packetData) == 0 {
		return nil, nil, nil, errors.New("empty packet")
	}

	switch packetData[0] >> 4 {
	case 4:
		parsedPacket = gopacket.NewPacket(packetData, layers.LayerTypeIPv4, gopacket.DecodeOptions{Lazy: true, NoCopy: true})
		if ipv4Layer := parsedPacket.Layer(layers.LayerTypeIPv4); ipv4Layer != nil {
			ipv4, _ := ipv4Layer.(*layers.IPv4)
			ipHeader = &packet.IPHeader{
				Version:  4,
				Protocol: packet.IPProtocol(ipv4.Protocol),
				Tos:      ipv4.TOS,
				TTL:      ipv4.TTL,
				Src:      ipv4.SrcIP,
				Dst:      ipv4.DstIP,
			}
		} else {
			var err error
			if errLayer := parsedPacket.ErrorLayer(); errLayer != nil {
				err = errLayer.Error()
			}
			return nil, nil, nil, fmt.Errorf("failed to parse IPv4 packet: %s", err)
		}
	case 6:
		parsedPacket = gopacket.NewPacket(packetData, layers.LayerTypeIPv6, gopacket.DecodeOptions{Lazy: true, NoCopy: true})
		if ipv6Layer := parsedPacket.Layer(layers.LayerTypeIPv6); ipv6Layer != nil {
			ipv6, _ := ipv6Layer.(*layers.IPv6)
			ipHeader = &packet.IPHeader{
				Version:  6,
				Protocol: packet.IPProtocol(ipv6.NextHeader),
				Tos:      ipv6.TrafficClass,
				TTL:      ipv6.HopLimit,
				Src:      ipv6.SrcIP,
				Dst:      ipv6.DstIP,
			}
		} else {
			var err error
			if errLayer := parsedPacket.ErrorLayer(); errLayer != nil {
				err = errLayer.Error()
			}
			return nil, nil, nil, fmt.Errorf("failed to parse IPv6 packet: %s", err)
		}
	default:
		return nil, nil, nil, errors.New("unknown IP version")
	}

	switch ipHeader.Protocol {
	case packet.TCP:
		if tcpLayer := parsedPacket.Layer(layers.LayerTypeTCP); tcpLayer != nil {
			tcp, _ := tcpLayer.(*layers.TCP)
			tpcUdpHeader = &packet.TCPUDPHeader{
				SrcPort:  uint16(tcp.SrcPort),
				DstPort:  uint16(tcp.DstPort),
				Checksum: tcp.Checksum,
			}
		} else {
			var err error
			if errLayer := parsedPacket.ErrorLayer(); errLayer != nil {
				err = errLayer.Error()
			}
			return nil, nil, nil, fmt.Errorf("could not parse TCP layer: %s", err)
		}
	case packet.UDP:
		if udpLayer := parsedPacket.Layer(layers.LayerTypeUDP); udpLayer != nil {
			udp, _ := udpLayer.(*layers.UDP)
			tpcUdpHeader = &packet.TCPUDPHeader{
				SrcPort:  uint16(udp.SrcPort),
				DstPort:  uint16(udp.DstPort),
				Checksum: udp.Checksum,
			}
		} else {
			var err error
			if errLayer := parsedPacket.ErrorLayer(); errLayer != nil {
				err = errLayer.Error()
			}
			return nil, nil, nil, fmt.Errorf("could not parse UDP layer: %s", err)
		}
	}

	if appLayer := parsedPacket.ApplicationLayer(); appLayer != nil {
		payload = appLayer.Payload()
	}

	if errLayer := parsedPacket.ErrorLayer(); errLayer != nil {
		return nil, nil, nil, errLayer.Error()
	}

	return
}
