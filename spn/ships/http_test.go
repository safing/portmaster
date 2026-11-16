package ships

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/safing/portmaster/spn/hub"
)

// TestHTTPShipUnresponsivePeer checks that launching an HTTP ship does not
// hang when the peer accepts the connection but never answers the upgrade.
func TestHTTPShipUnresponsivePeer(t *testing.T) {
	t.Parallel()

	// Accept connections, but never respond.
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = listener.Close()
	})
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			t.Cleanup(func() {
				_ = conn.Close()
			})
		}
	}()

	listenAddr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("unexpected listener address type %T", listener.Addr())
	}
	transport := &hub.Transport{
		Protocol: "http",
		Port:     uint16(listenAddr.Port), //nolint:gosec // Port is always in range.
	}

	tests := []struct {
		name   string
		newCtx func() (context.Context, context.CancelFunc)
	}{
		{
			name: "deadline",
			newCtx: func() (context.Context, context.CancelFunc) {
				return context.WithTimeout(context.Background(), 200*time.Millisecond)
			},
		},
		{
			name: "cancel",
			newCtx: func() (context.Context, context.CancelFunc) {
				ctx, cancel := context.WithCancel(context.Background())
				time.AfterFunc(200*time.Millisecond, cancel)
				return ctx, cancel
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := tc.newCtx()
			defer cancel()

			result := make(chan error, 1)
			go func() {
				ship, err := launchHTTPShip(ctx, transport, localhost)
				if ship != nil {
					ship.Sink()
				}
				result <- err
			}()

			select {
			case err := <-result:
				if err == nil {
					t.Fatal("expected launch to fail against an unresponsive peer")
				}
			case <-time.After(5 * time.Second):
				t.Fatal("launch did not return after ctx was done")
			}
		})
	}
}
