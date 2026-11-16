package state

import (
	"net"
	"testing"

	"github.com/safing/portmaster/service/network/socket"
)

func newBenchUDPTables() (udp4 *udpTable) {
	binds4 := make([]*socket.BindInfo, 0, 50)
	binds6 := make([]*socket.BindInfo, 0, 51)
	for i := range uint16(50) {
		binds4 = append(binds4, &socket.BindInfo{
			Local: socket.Address{IP: net.IPv4(127, 0, 0, 1), Port: 40000 + i},
			PID:   1000,
		})
		binds6 = append(binds6, &socket.BindInfo{
			Local: socket.Address{IP: net.IPv6loopback, Port: 41000 + i},
			PID:   1000,
		})
	}
	binds6 = append(binds6, &socket.BindInfo{
		Local: socket.Address{IP: net.IPv6zero, Port: 5353},
		PID:   4242,
	})
	udp4 = newTestUDPTable(4, binds4)
	udp4.dualStack = newTestUDPTable(6, binds6)
	return udp4
}

func BenchmarkUDPLookupDualStack(b *testing.B) {
	udp4 := newBenchUDPTables()
	pkt := udp4TestPacket(false)

	b.ReportAllocs()
	for b.Loop() {
		_, _, err := udp4.lookup(pkt, true)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkUDPGetDirection(b *testing.B) {
	udp4 := newBenchUDPTables()
	pkt := udp4TestPacket(false)
	bind := &socket.BindInfo{Local: socket.Address{IP: net.IPv6zero, Port: 5353}}

	b.ReportAllocs()
	for b.Loop() {
		udp4.getDirection(bind, pkt)
	}
}
