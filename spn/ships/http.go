package ships

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/spn/conf"
	"github.com/safing/portmaster/spn/hub"
)

// HTTPShip is a ship that uses HTTP.
type HTTPShip struct {
	ShipBase
}

// HTTPPier is a pier that uses HTTP.
type HTTPPier struct {
	PierBase

	newDockings chan net.Conn
}

func init() {
	Register("http", &Builder{
		LaunchShip:    launchHTTPShip,
		EstablishPier: establishHTTPPier,
	})
}

/*
HTTP Transport Variants:

1. Hijack connection and switch to raw SPN protocol:

Request:

		GET <path> HTTP/1.1
		Connection: Upgrade
		Upgrade: SPN

Response:

		HTTP/1.1 101 Switching Protocols
		Connection: Upgrade
		Upgrade: SPN

*/

func launchHTTPShip(ctx context.Context, transport *hub.Transport, ip net.IP) (Ship, error) {
	// Default to root path.
	path := transport.Path
	if path == "" {
		path = "/"
	}

	// Build request for Variant 1.
	variant := 1
	request, err := http.NewRequest(http.MethodGet, path, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to build HTTP request: %w", err)
	}
	request.Header.Set("Connection", "Upgrade")
	request.Header.Set("Upgrade", "SPN")

	// Create connection.
	var dialNet string
	if ip4 := ip.To4(); ip4 != nil {
		dialNet = "tcp4"
	} else {
		dialNet = "tcp6"
	}
	dialer := &net.Dialer{
		Timeout:       30 * time.Second,
		LocalAddr:     conf.GetBindAddr(dialNet),
		FallbackDelay: -1, // Disables Fast Fallback from IPv6 to IPv4.
		KeepAlive:     -1, // Disable keep-alive.
	}
	conn, err := dialer.DialContext(ctx, dialNet, net.JoinHostPort(ip.String(), portToA(transport.Port)))
	if err != nil {
		return nil, fmt.Errorf("failed to connect: %w", err)
	}

	// Send HTTP request.
	err = request.Write(conn)
	if err != nil {
		return nil, fmt.Errorf("failed to send HTTP request: %w", err)
	}

	// Receive HTTP response.
	response, err := http.ReadResponse(bufio.NewReader(conn), request)
	if err != nil {
		return nil, fmt.Errorf("failed to read HTTP response: %w", err)
	}
	defer response.Body.Close() //nolint:errcheck,gosec

	// Handle response according to variant.
	switch variant {
	case 1:
		if response.StatusCode == http.StatusSwitchingProtocols &&
			response.Header.Get("Connection") == "Upgrade" &&
			response.Header.Get("Upgrade") == "SPN" {
			// Continue
		} else {
			return nil, fmt.Errorf("received unexpected response for variant 1: %s", response.Status)
		}

	default:
		return nil, fmt.Errorf("internal error: unsupported http transport variant: %d", variant)
	}

	// Create ship.
	ship := &HTTPShip{
		ShipBase: ShipBase{
			conn:      conn,
			transport: transport,
			mine:      true,
			secure:    false,
		},
	}

	// Init and return.
	ship.calculateLoadSize(ip, nil, TCPHeaderMTUSize)
	ship.initBase()
	return ship, nil
}

func (pier *HTTPPier) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodGet &&
		r.Header.Get("Connection") == "Upgrade" &&
		r.Header.Get("Upgrade") == "SPN":
		// Request for Variant 1.

		// Hijack connection.
		var conn net.Conn
		if hijacker, ok := w.(http.Hijacker); ok {
			// Empty body, so the hijacked connection starts with a clean buffer.
			_, err := io.ReadAll(r.Body)
			if err != nil {
				http.Error(w, "", http.StatusInternalServerError)
				log.Warningf("ships: failed to empty body for hijack for %s: %s", r.RemoteAddr, err)
				return
			}
			_ = r.Body.Close()

			// Reply with upgrade confirmation.
			w.Header().Set("Connection", "Upgrade")
			w.Header().Set("Upgrade", "SPN")
			w.WriteHeader(http.StatusSwitchingProtocols)

			// Get connection.
			conn, _, err = hijacker.Hijack()
			if err != nil {
				log.Warningf("ships: failed to hijack http connection from %s: %s", r.RemoteAddr, err)
				return
			}
		} else {
			http.Error(w, "", http.StatusInternalServerError)
			log.Warningf("ships: connection from %s cannot be hijacked", r.RemoteAddr)
			return
		}

		// Create new ship.
		ship := &HTTPShip{
			ShipBase: ShipBase{
				transport: pier.transport,
				conn:      conn,
				mine:      false,
				secure:    false,
			},
		}
		ship.calculateLoadSize(nil, conn.RemoteAddr(), TCPHeaderMTUSize)
		ship.initBase()

		// Submit new docking request.
		select {
		case pier.dockingRequests <- ship:
		case <-r.Context().Done():
			return
		}

	default:
		// Reply with info page if no variant matches the request.
		ServeInfoPage(w, r)
	}
}

func establishHTTPPier(transport *hub.Transport, dockingRequests chan Ship) (Pier, error) {
	// Default to root path.
	path := transport.Path
	if path == "" {
		path = "/"
	}

	// Create pier.
	pier := &HTTPPier{
		newDockings: make(chan net.Conn),
		PierBase: PierBase{
			transport:       transport,
			dockingRequests: dockingRequests,
		},
	}
	pier.initBase()

	// Register handler.
	err := addHTTPHandler(transport.Port, path, pier.ServeHTTP)
	if err != nil {
		return nil, fmt.Errorf("failed to add HTTP handler: %w", err)
	}

	return pier, nil
}

// Abolish closes the underlying listener and cleans up any related resources.
func (pier *HTTPPier) Abolish() {
	// Only abolish once.
	if !pier.abolishing.SetToIf(false, true) {
		return
	}

	// Do not close the listener, as it is shared.
	// Instead, remove the HTTP handler and the shared server will shutdown itself when needed.
	_ = removeHTTPHandler(pier.transport.Port, pier.transport.Path)
}
