package navigator

import (
	"net"
	"net/url"
	"testing"

	"github.com/safing/portmaster/service/intel"
	"github.com/safing/portmaster/service/network/packet"
	"github.com/safing/portmaster/service/profile/endpoints"
)

func TestParseRouteSimConn(t *testing.T) {
	t.Parallel()

	tests := []struct {
		query   string
		want    routeSimConn
		wantErr bool
	}{
		// Defaults, same as before the parameters were added.
		{query: "", want: routeSimConn{}},
		// Port: protocol defaults to TCP, encrypted is derived from the port.
		{query: "port=443", want: routeSimConn{port: 443, protocol: packet.TCP, encrypted: true}},
		{query: "port=80", want: routeSimConn{port: 80, protocol: packet.TCP}},
		{query: "port=53&protocol=UDP", want: routeSimConn{port: 53, protocol: packet.UDP, protocolGiven: true}},
		{query: "protocol=udp", want: routeSimConn{protocol: packet.UDP, protocolGiven: true}},
		// Encrypted: without a value it counts as true, a given value wins over the port.
		{query: "encrypted", want: routeSimConn{encrypted: true, encryptedGiven: true}},
		{query: "encrypted=true", want: routeSimConn{encrypted: true, encryptedGiven: true}},
		{query: "encrypted=false", want: routeSimConn{encryptedGiven: true}},
		{query: "port=443&encrypted=false", want: routeSimConn{port: 443, protocol: packet.TCP, encryptedGiven: true}},
		// Invalid values.
		{query: "port=0", wantErr: true},
		{query: "port=65536", wantErr: true},
		{query: "port=http", wantErr: true},
		{query: "protocol=icmp", wantErr: true},
		{query: "encrypted=maybe", wantErr: true},
	}

	for _, tc := range tests {
		query, err := url.ParseQuery(tc.query)
		if err != nil {
			t.Fatal(err)
		}

		got, err := parseRouteSimConn(query)
		switch {
		case tc.wantErr:
			if err == nil {
				t.Errorf("%q: expected an error, got %+v", tc.query, *got)
			}
		case err != nil:
			t.Errorf("%q: unexpected error: %s", tc.query, err)
		case *got != tc.want:
			t.Errorf("%q: got %+v, want %+v", tc.query, *got, tc.want)
		}
	}
}

// TestRouteSimConnExitPolicy checks that the connection details of a route
// simulation request reach the exit policy check of a hub.
func TestRouteSimConnExitPolicy(t *testing.T) {
	t.Parallel()

	// Exit policy of a hub that only allows some ports.
	const denyAll = "- *"
	exitPolicy, err := endpoints.ParseEndpoints([]string{"+ * */80", "+ * TCP/443", denyAll})
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		query string
		want  endpoints.EPResult
	}{
		// Without a port, no rule with a port matches.
		{query: "", want: endpoints.Denied},
		{query: "port=80", want: endpoints.Permitted},
		{query: "port=80&protocol=udp", want: endpoints.Permitted},
		// The protocol defaults to TCP.
		{query: "port=443", want: endpoints.Permitted},
		{query: "port=443&protocol=udp", want: endpoints.Denied},
		{query: "port=22", want: endpoints.Denied},
	}

	for _, tc := range tests {
		query, err := url.ParseQuery(tc.query)
		if err != nil {
			t.Fatal(err)
		}
		simConn, err := parseRouteSimConn(query)
		if err != nil {
			t.Fatal(err)
		}

		// Set up the destination the same way as the request handler.
		entity := &intel.Entity{IP: net.IPv4(198, 51, 100, 1)}
		entity.Protocol = uint8(simConn.protocol)
		entity.Port = simConn.port
		entity.Init(0)

		if got := endpointListMatch(exitPolicy, entity); got != tc.want {
			t.Errorf("%q: got %s, want %s", tc.query, got, tc.want)
		}
	}
}
