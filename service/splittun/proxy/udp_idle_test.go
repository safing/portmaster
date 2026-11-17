package proxy

import (
	"context"
	"net"
	"testing"
	"time"
)

// TestUDPProxy_OneWayFlowKeepsSession checks that a client which keeps sending
// to a silent upstream keeps its NAT session (and thus its upstream source
// port) instead of having it torn down and recreated every ReadTimeout.
func TestUDPProxy_OneWayFlowKeepsSession(t *testing.T) {
	t.Parallel()

	sink, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer sink.Close() //nolint:errcheck
	go func() {
		buf := make([]byte, 2048)
		for {
			_, _, readErr := sink.ReadFromUDP(buf)
			if readErr != nil {
				return
			}
		}
	}()

	cfg := DefaultConfig()
	cfg.ReadTimeout = 150 * time.Millisecond
	p, err := NewUDPProxyWithConfig("127.0.0.1:0", "udp4", passThroughDecider(sink.LocalAddr().String()), nil, cfg, "")
	if err != nil {
		t.Fatalf("NewUDPProxy: %v", err)
	}
	defer p.Shutdown(context.Background()) //nolint:errcheck

	proxyAddr, ok := p.Addr().(*net.UDPAddr)
	if !ok {
		t.Fatalf("unexpected addr type %T", p.Addr())
	}
	client, err := net.DialUDP("udp", nil, proxyAddr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer client.Close() //nolint:errcheck

	// Send for about 3x ReadTimeout while the upstream stays silent.
	for range 15 {
		_, err = client.Write([]byte("ping"))
		if err != nil {
			t.Fatalf("write: %v", err)
		}
		time.Sleep(30 * time.Millisecond)
	}

	if created := p.Metrics().TotalCreated; created != 1 {
		t.Fatalf("active one-way flow was recreated: %d sessions created, want 1", created)
	}
}
