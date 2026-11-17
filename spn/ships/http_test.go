package ships

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/safing/portmaster/spn/hub"
)

const httpProtocol = "http"

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
		Protocol: httpProtocol,
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

// TestHTTPPierKeepsDataSentWithRequest checks that the pier does not lose data
// the client sends in the same packet as the upgrade request.
func TestHTTPPierKeepsDataSentWithRequest(t *testing.T) {
	t.Parallel()

	dockingRequests := make(chan Ship, 1)
	pier := &HTTPPier{
		PierBase: PierBase{
			transport:       &hub.Transport{Protocol: "http"},
			dockingRequests: dockingRequests,
		},
	}
	server := httptest.NewServer(pier)
	t.Cleanup(server.Close)

	conn, err := net.Dial("tcp", server.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	payload := "SPN payload sent right after the request"
	_, err = io.WriteString(conn, "GET / HTTP/1.1\r\nHost: test\r\nConnection: Upgrade\r\nUpgrade: SPN\r\n\r\n"+payload)
	if err != nil {
		t.Fatal(err)
	}

	var ship Ship
	select {
	case ship = <-dockingRequests:
	case <-time.After(5 * time.Second):
		t.Fatal("no docking request")
	}
	t.Cleanup(ship.Sink)

	readPayload(t, ship.(*HTTPShip).conn, payload) //nolint:forcetypeassert
}

// TestHTTPShipKeepsDataSentWithResponse checks that the ship does not lose data
// the server sends in the same packet as the upgrade response.
func TestHTTPShipKeepsDataSentWithResponse(t *testing.T) {
	t.Parallel()

	payload := "SPN payload sent right after the response"
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		t.Cleanup(func() { _ = conn.Close() })
		// Read the request, then reply and send the payload in one write.
		_, err = http.ReadRequest(bufio.NewReader(conn))
		if err != nil {
			return
		}
		_, _ = io.WriteString(conn, "HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: SPN\r\n\r\n"+payload)
	}()

	listenAddr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("unexpected listener address type %T", listener.Addr())
	}
	transport := &hub.Transport{
		Protocol: httpProtocol,
		Port:     uint16(listenAddr.Port), //nolint:gosec // Port is always in range.
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ship, err := launchHTTPShip(ctx, transport, localhost)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ship.Sink)

	readPayload(t, ship.(*HTTPShip).conn, payload) //nolint:forcetypeassert
}

func readPayload(t *testing.T, conn net.Conn, want string) {
	t.Helper()

	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	got := make([]byte, len(want))
	_, err := io.ReadFull(conn, got)
	if err != nil {
		t.Fatalf("failed to read payload: %s (got %q)", err, got)
	}
	if string(got) != want {
		t.Fatalf("got payload %q, want %q", got, want)
	}
}
