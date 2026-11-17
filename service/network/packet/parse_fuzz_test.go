package packet

import (
	"bytes"
	"net"
	"testing"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
)

// serializeSeed builds a seed packet from the given gopacket layers.
func serializeSeed(tb testing.TB, ls ...gopacket.SerializableLayer) []byte {
	tb.Helper()

	buf := gopacket.NewSerializeBuffer()
	opts := gopacket.SerializeOptions{FixLengths: true, ComputeChecksums: true}
	err := gopacket.SerializeLayers(buf, opts, ls...)
	if err != nil {
		tb.Fatalf("failed to serialize seed: %s", err)
	}
	return append([]byte(nil), buf.Bytes()...)
}

// layer3Seeds returns valid IPv4/IPv6 packets covering every protocol the
// parser knows about, plus some unusual but valid shapes (fragments,
// extension headers, protocol 61).
func layer3Seeds(tb testing.TB) [][]byte {
	tb.Helper()

	src4, dst4 := net.IPv4(192, 168, 1, 2).To4(), net.IPv4(1, 1, 1, 1).To4()
	src6, dst6 := net.ParseIP("fd00::2"), net.ParseIP("2001:db8::1")
	ip4 := func(proto layers.IPProtocol) *layers.IPv4 {
		return &layers.IPv4{Version: 4, IHL: 5, TTL: 64, Protocol: proto, SrcIP: src4, DstIP: dst4}
	}
	ip6 := func(next layers.IPProtocol) *layers.IPv6 {
		return &layers.IPv6{Version: 6, HopLimit: 64, NextHeader: next, SrcIP: src6, DstIP: dst6}
	}
	payload := gopacket.Payload("hello")

	seeds := make([][]byte, 0, 11)

	// IPv4.
	v4 := ip4(layers.IPProtocolTCP)
	tcp := &layers.TCP{SrcPort: 40000, DstPort: 443, SYN: true, Window: 1024}
	_ = tcp.SetNetworkLayerForChecksum(v4)
	seeds = append(seeds, serializeSeed(tb, v4, tcp, payload))

	v4 = ip4(layers.IPProtocolUDP)
	udp := &layers.UDP{SrcPort: 40001, DstPort: 53}
	_ = udp.SetNetworkLayerForChecksum(v4)
	seeds = append(seeds, serializeSeed(tb, v4, udp, payload))

	seeds = append(seeds, serializeSeed(tb, ip4(layers.IPProtocolICMPv4),
		&layers.ICMPv4{TypeCode: layers.CreateICMPv4TypeCode(layers.ICMPv4TypeEchoRequest, 0), Id: 1, Seq: 1}, payload))
	seeds = append(seeds, serializeSeed(tb, ip4(layers.IPProtocolIGMP),
		gopacket.Payload{0x11, 0x64, 0xee, 0x9b, 0, 0, 0, 0})) // IGMPv2 membership query
	seeds = append(seeds, serializeSeed(tb, ip4(layers.IPProtocol(61)), payload))

	// IPv4 non-first fragment of a TCP packet.
	frag := ip4(layers.IPProtocolTCP)
	frag.FragOffset = 10
	seeds = append(seeds, serializeSeed(tb, frag, payload))

	// IPv6.
	v6 := ip6(layers.IPProtocolTCP)
	tcp = &layers.TCP{SrcPort: 40002, DstPort: 80, ACK: true, Window: 1024}
	_ = tcp.SetNetworkLayerForChecksum(v6)
	seeds = append(seeds, serializeSeed(tb, v6, tcp, payload))

	v6 = ip6(layers.IPProtocolUDP)
	udp = &layers.UDP{SrcPort: 40003, DstPort: 53}
	_ = udp.SetNetworkLayerForChecksum(v6)
	seeds = append(seeds, serializeSeed(tb, v6, udp, payload))

	v6 = ip6(layers.IPProtocolICMPv6)
	icmp6 := &layers.ICMPv6{TypeCode: layers.CreateICMPv6TypeCode(layers.ICMPv6TypeEchoRequest, 0)}
	_ = icmp6.SetNetworkLayerForChecksum(v6)
	seeds = append(seeds, serializeSeed(tb, v6, icmp6, &layers.ICMPv6Echo{Identifier: 1, SeqNumber: 1}, payload))

	// IPv6 with a hop-by-hop extension header in front of UDP.
	v6 = ip6(layers.IPProtocolIPv6HopByHop)
	hbh := &layers.IPv6HopByHop{}
	hbh.NextHeader = layers.IPProtocolUDP
	hbh.Options = []*layers.IPv6HopByHopOption{{OptionType: 1, OptionLength: 4, OptionData: []byte{0, 0, 0, 0}}}
	udp = &layers.UDP{SrcPort: 40004, DstPort: 5353}
	_ = udp.SetNetworkLayerForChecksum(v6)
	seeds = append(seeds, serializeSeed(tb, v6, hbh, udp, payload))

	// IPv6 fragment header (first fragment) in front of TCP.
	seeds = append(seeds, serializeSeed(tb, ip6(layers.IPProtocolIPv6Fragment),
		gopacket.Payload{byte(layers.IPProtocolTCP), 0, 0, 1, 0, 0, 0, 1}, payload))

	return seeds
}

// checkParsedInfo checks invariants that must hold after successful parsing
// and exercises the accessors the platform-neutral callers use.
func checkParsedInfo(t *testing.T, base *Base) {
	t.Helper()

	info := base.Info()
	pkt := base.Layers()
	if pkt == nil {
		t.Fatal("parsing succeeded, but no layers were saved")
	}

	if transport := pkt.TransportLayer(); transport != nil {
		switch tl := transport.(type) {
		case *layers.TCP:
			if info.Protocol != TCP || info.SrcPort != uint16(tl.SrcPort) || info.DstPort != uint16(tl.DstPort) {
				t.Fatalf("TCP layer does not match info: %+v", info)
			}
		case *layers.UDP:
			if info.Protocol != UDP || info.SrcPort != uint16(tl.SrcPort) || info.DstPort != uint16(tl.DstPort) {
				t.Fatalf("UDP layer does not match info: %+v", info)
			}
		}
		if !bytes.Equal(base.Payload(), transport.LayerPayload()) {
			t.Fatal("payload does not match transport payload")
		}
	}

	// Accessors used by the firewall and netenv.
	_ = pkt.LayerClass(layers.LayerClassIPControl)
	_ = pkt.Layer(layers.LayerTypeICMPv4)
	_ = pkt.Layer(layers.LayerTypeICMPv6)
	_ = base.GetConnectionID()
	_ = base.FmtPacket()
	_ = base.FmtRemoteAddress()
	_ = base.HasPorts()
}

// clip returns a copy of data without spare capacity.
func clip(data []byte) []byte {
	c := make([]byte, len(data))
	copy(c, data)
	return c
}

// compareParseResults fails if two parses of the same packet differ.
func compareParseResults(t *testing.T, a *Base, aErr error, b *Base, bErr error) {
	t.Helper()

	if (aErr == nil) != (bErr == nil) {
		t.Fatalf("parse result depends on buffer capacity: %v vs %v (%+v / %+v)", aErr, bErr, a.Info(), b.Info())
	}
	ai, bi := a.Info(), b.Info()
	if ai.Version != bi.Version || ai.Protocol != bi.Protocol ||
		ai.SrcPort != bi.SrcPort || ai.DstPort != bi.DstPort ||
		!ai.Src.Equal(bi.Src) || !ai.Dst.Equal(bi.Dst) {
		t.Fatalf("parsed info depends on buffer capacity: %+v vs %+v", ai, bi)
	}
	if !bytes.Equal(a.Payload(), b.Payload()) {
		t.Fatal("payload depends on buffer capacity")
	}
}

func FuzzParseLayer3(f *testing.F) {
	for _, seed := range layer3Seeds(f) {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		// Use a buffer without spare capacity, so that parsing is deterministic.
		data = clip(data)
		orig := clip(data)

		base := &Base{}
		err := ParseLayer3(data, base)

		// Parsing must not depend on the spare capacity of the given buffer.
		padded := make([]byte, len(data), len(data)+256)
		copy(padded, data)
		paddedBase := &Base{}
		paddedErr := ParseLayer3(padded, paddedBase)
		compareParseResults(t, base, err, paddedBase, paddedErr)

		if !bytes.Equal(orig, data) {
			t.Fatal("parsing modified the packet data")
		}
		if err != nil {
			return
		}

		info := base.Info()
		switch data[0] >> 4 {
		case 4:
			if info.Version != IPv4 || len(info.Src) != net.IPv4len || len(info.Dst) != net.IPv4len {
				t.Fatalf("bad IPv4 info: %+v", info)
			}
		case 6:
			if info.Version != IPv6 || len(info.Src) != net.IPv6len || len(info.Dst) != net.IPv6len {
				t.Fatalf("bad IPv6 info: %+v", info)
			}
		default:
			t.Fatalf("accepted unknown IP version %d", data[0]>>4)
		}
		checkParsedInfo(t, base)

		// The lazy parse must yield the same result as an eager one.
		eager := gopacket.NewPacket(orig, base.Layers().Layers()[0].LayerType(), gopacket.Default)
		if nl := eager.NetworkLayer(); nl == nil || nl.NetworkFlow() != base.Layers().NetworkLayer().NetworkFlow() {
			t.Fatal("lazy and eager parse disagree on the network layer")
		}
		if eager.ErrorLayer() != nil && info.Protocol != AnyHostInternalProtocol61 {
			t.Fatalf("eager parse failed, lazy parse did not: %s", eager.ErrorLayer().Error())
		}
	})
}

func FuzzParseLayer4(f *testing.F) {
	// Seed with the transport parts of the layer 3 seeds.
	for _, seed := range layer3Seeds(f) {
		pkt := gopacket.NewPacket(seed, layers.LayerTypeIPv4, gopacket.Default)
		if seed[0]>>4 == 6 {
			pkt = gopacket.NewPacket(seed, layers.LayerTypeIPv6, gopacket.Default)
		}
		nl := pkt.NetworkLayer()
		if nl == nil || len(nl.LayerPayload()) == 0 {
			continue
		}
		var proto byte
		switch ip := nl.(type) {
		case *layers.IPv4:
			proto = byte(ip.Protocol)
		case *layers.IPv6:
			proto = byte(ip.NextHeader)
		}
		f.Add(proto, nl.LayerPayload())
	}

	f.Fuzz(func(t *testing.T, proto byte, data []byte) {
		data = clip(data)
		base := &Base{}
		base.Info().Protocol = IPProtocol(proto)
		err := ParseLayer4(data, base)

		padded := make([]byte, len(data), len(data)+256)
		copy(padded, data)
		paddedBase := &Base{}
		paddedBase.Info().Protocol = IPProtocol(proto)
		paddedErr := ParseLayer4(padded, paddedBase)
		compareParseResults(t, base, err, paddedBase, paddedErr)

		if err != nil {
			return
		}

		switch IPProtocol(proto) { //nolint:exhaustive // Only supported protocols may be parsed.
		case TCP, UDP, ICMP, ICMPv6, IGMP:
		default:
			t.Fatalf("accepted unsupported protocol %d", proto)
		}
		if base.Info().Protocol != IPProtocol(proto) {
			t.Fatalf("protocol changed from %d to %d", proto, base.Info().Protocol)
		}
		checkParsedInfo(t, base)
	})
}
