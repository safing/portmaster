package resolver

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/miekg/dns"
)

// TestPlainResolverPreAuthenticatesBoundPort checks that the plain resolver
// pre-authenticates the local port the OS assigned to its socket, which is the
// port the server sees the query coming from, and releases it afterwards.
//
// The resolver reports ports through the package-level hooks, so the test
// replaces them with recording functions and queries a local DNS server that
// reports the source port of the query.
func TestPlainResolverPreAuthenticatesBoundPort(t *testing.T) { //nolint:paralleltest // Replaces the package-level hooks.
	serverAddr, seenPort := startRecordingDNSServer(t)

	// Record the hook calls by port. The started module may run other UDP
	// queries at the same time, so ports of other queries may show up too.
	var (
		hooksLock  sync.Mutex
		authorized = make(map[uint16]string) // port -> network
		released   = make(map[uint16]bool)   // port -> marked unusable
	)
	previousHooks := localPortHooks
	localPortHooks = LocalPortHooks{
		Authorize: func(network string, port uint16) {
			hooksLock.Lock()
			defer hooksLock.Unlock()
			authorized[port] = network
		},
		Release: func(network string, port uint16, unusable bool) {
			hooksLock.Lock()
			defer hooksLock.Unlock()
			released[port] = unusable
		},
	}
	t.Cleanup(func() { localPortHooks = previousHooks })

	// Query the local server through the plain resolver.
	pr := NewPlainResolver(&Resolver{
		ServerAddress: serverAddr.String(),
		Info: &ResolverInfo{
			Name:   "test-plain",
			Type:   ServerTypeDNS,
			Source: ServerSourceConfigured,
			IP:     serverAddr.IP,
			Port:   uint16(serverAddr.Port), //nolint:gosec // Ports of net.Addr are within the uint16 range.
		},
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := pr.Query(ctx, &Query{FQDN: "example.com.", QType: dns.Type(dns.TypeA)})
	if err != nil {
		t.Fatalf("query failed: %s", err)
	}

	// The query was answered, so the server has already reported its port.
	port := <-seenPort

	// The port the server saw must have been authorized for udp and released
	// as usable, as binding it succeeded.
	hooksLock.Lock()
	defer hooksLock.Unlock()
	switch network, ok := authorized[port]; {
	case !ok:
		t.Errorf("the query came from port %d, but that port was not authorized (authorized: %v)", port, authorized)
	case network != networkUDP:
		t.Errorf("port %d was authorized for network %q, expected %q", port, network, networkUDP)
	}
	switch unusable, ok := released[port]; {
	case !ok:
		t.Errorf("port %d was not released after the query (released: %v)", port, released)
	case unusable:
		t.Errorf("port %d was bound successfully, but released as unusable", port)
	}
}

// startRecordingDNSServer listens on a loopback UDP port, answers the first
// query with an empty reply and sends the source port of that query on the
// returned channel. The port is sent before the reply, so it is available as
// soon as the query has been answered.
func startRecordingDNSServer(t *testing.T) (*net.UDPAddr, <-chan uint16) {
	t.Helper()

	pc, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %s", err)
	}
	t.Cleanup(func() { _ = pc.Close() })
	serverAddr, ok := pc.LocalAddr().(*net.UDPAddr)
	if !ok {
		t.Fatalf("unexpected local address type %T", pc.LocalAddr())
	}

	seenPort := make(chan uint16, 1)
	go func() {
		buf := make([]byte, dns.MinMsgSize)
		n, from, err := pc.ReadFrom(buf)
		if err != nil {
			// The listener was closed by the cleanup.
			return
		}
		if fromAddr, ok := from.(*net.UDPAddr); ok {
			seenPort <- uint16(fromAddr.Port) //nolint:gosec // Ports of net.Addr are within the uint16 range.
		}

		query := new(dns.Msg)
		err = query.Unpack(buf[:n])
		if err != nil {
			t.Errorf("failed to unpack query: %s", err)
			return
		}
		reply, err := new(dns.Msg).SetReply(query).Pack()
		if err != nil {
			t.Errorf("failed to pack reply: %s", err)
			return
		}
		_, err = pc.WriteTo(reply, from)
		if err != nil {
			t.Errorf("failed to send reply: %s", err)
		}
	}()

	return serverAddr, seenPort
}
