package state

import (
	"net"
	"testing"
	"time"

	"github.com/safing/portmaster/service/network/packet"
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

// BenchmarkExistsCleanerRun measures the Exists() checks of one cleaner run
// with 500 connections on a system with 2000 sockets per table.
func BenchmarkExistsCleanerRun(b *testing.B) {
	const (
		sockets = 2000
		checks  = 500
	)

	connections := make([]*socket.ConnectionInfo, 0, sockets)
	binds := make([]*socket.BindInfo, 0, sockets)
	for i := range uint16(sockets) {
		connections = append(connections, &socket.ConnectionInfo{
			Local:  socket.Address{IP: net.IPv4(192, 168, 1, 10), Port: 30000 + i},
			Remote: socket.Address{IP: net.IPv4(9, 9, 9, 9), Port: 443},
		})
		binds = append(binds, &socket.BindInfo{
			Local: socket.Address{IP: net.IPv4(192, 168, 1, 10), Port: 30000 + i},
		})
	}
	tcp4 := newTestTCPTable(4, connections)
	udp4 := newTestUDPTable(4, binds)
	// Use the time of the table update, so that the tables are not updated.
	now := time.Now()

	// Check every fourth socket, so that the matches are spread over the
	// tables, as the connections of the cleaner are.
	tcpPkts := make([]*packet.Info, 0, checks)
	udpPkts := make([]*packet.Info, 0, checks)
	for i := range checks {
		bind := binds[i*sockets/checks]
		port := bind.Local.Port
		tcpPkts = append(tcpPkts, cleanerPacket(packet.IPv4, packet.TCP, testLocal4, port, testRemote4, 443))
		udpPkt := cleanerPacket(packet.IPv4, packet.UDP, testLocal4, port, testRemote4, 443)
		udpPkts = append(udpPkts, udpPkt)
		udp4.getDirection(bind, udpPkt)
	}

	b.Run("TCP", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			for _, pkt := range tcpPkts {
				if !tcp4.exists(pkt, now) {
					b.Fatal("connection not found")
				}
			}
		}
	})
	b.Run("UDP", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			for _, pkt := range udpPkts {
				if !udp4.exists(pkt, now) {
					b.Fatal("connection not found")
				}
			}
		}
	})
}
