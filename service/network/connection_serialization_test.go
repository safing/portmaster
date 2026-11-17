package network

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"flag"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/safing/portmaster/service/intel"
	"github.com/safing/portmaster/service/network/netutils"
	"github.com/safing/portmaster/service/network/packet"
	"github.com/safing/portmaster/service/resolver"
	"github.com/safing/structures/dsd"
)

var updateGolden = flag.Bool("update-golden", false, "rewrite the serialization golden files")

// serializationTestConnections returns connections that cover all values of
// the verdict state that is mirrored for the packet path.
func serializationTestConnections() map[string]*Connection {
	outbound := &Connection{
		ID:           "6-192.168.1.10-40000-1.1.1.1-443",
		Type:         IPConnection,
		Scope:        "example.com.",
		IPVersion:    packet.IPv4,
		Inbound:      false,
		IPProtocol:   packet.TCP,
		LocalIP:      net.IPv4(192, 168, 1, 10),
		LocalIPScope: netutils.SiteLocal,
		LocalPort:    40000,
		PID:          4242,
		Entity: &intel.Entity{
			Protocol: 6,
			Port:     443,
			Domain:   "example.com.",
			IP:       net.IPv4(1, 1, 1, 1),
			IPScope:  netutils.Global,
		},
		Resolver: &resolver.ResolverInfo{
			Name: "test", //nolint:goconst // Test data.
			Type: resolver.ServerTypeDoT,
		},
		Verdict:               VerdictAccept,
		ConnectionEstablished: true,
		Reason: Reason{
			Msg:       "allowed by rule",
			OptionKey: "filter/endpoints",
			Profile:   "core:profiles/local/test",
		},
		Started:          1700000000,
		VerdictPermanent: true,
		Encrypted:        true,
		ProcessContext: ProcessContext{
			ProcessName: "test",
			ProfileName: "Test",
			BinaryPath:  "/usr/bin/test",
			CmdLine:     "/usr/bin/test --flag",
			PID:         4242,
			CreatedAt:   1699999999,
			Profile:     "test",
			Source:      "local", //nolint:goconst // Test data.
		},
		HistoryEnabled:         true,
		BandwidthEnabled:       true,
		BytesReceived:          1234,
		BytesSent:              5678,
		ProfileRevisionCounter: 7,
	}

	inbound := &Connection{
		ID:           "17-192.168.1.10-53-192.168.1.20-50000",
		Type:         IPConnection,
		Scope:        IncomingLAN,
		IPVersion:    packet.IPv4,
		Inbound:      true,
		IPProtocol:   packet.UDP,
		LocalIP:      net.IPv4(192, 168, 1, 10),
		LocalIPScope: netutils.SiteLocal,
		LocalPort:    53,
		PID:          -1,
		Entity: &intel.Entity{
			Protocol: 17,
			Port:     50000,
			IP:       net.IPv4(192, 168, 1, 20),
			IPScope:  netutils.SiteLocal,
		},
		Verdict: VerdictDrop,
		Reason: Reason{
			Msg: "incoming connection blocked",
		},
		Started:  1700000100,
		Ended:    1700000200,
		Internal: true,
	}

	undecided := &Connection{
		ID:         "1-192.168.1.10-0-8.8.8.8-0",
		Type:       IPConnection,
		IPVersion:  packet.IPv4,
		IPProtocol: packet.ICMP,
		PID:        0,
		Verdict:    VerdictUndecided,
		Started:    1700000300,
	}

	rerouted := &Connection{
		ID:         "dns-request",
		Type:       DNSRequest,
		Scope:      "example.org.",
		PID:        2147483000,
		Verdict:    VerdictRerouteToSplitTun,
		Started:    1700000400,
		Ended:      1700000400,
		Tunneled:   true,
		Inspecting: true,
	}

	return map[string]*Connection{
		"outbound":  outbound,
		"inbound":   inbound,
		"undecided": undecided,
		"rerouted":  rerouted,
	}
}

// TestConnectionSerializationGolden pins the serialized representation of
// connections, as it is seen by the API, the UI, netquery and database
// subscribers. The golden files were created before the verdict state was
// mirrored for the packet path and must not change with internal changes to
// the connection.
func TestConnectionSerializationGolden(t *testing.T) {
	t.Parallel()

	formats := map[string]func(conn *Connection) ([]byte, error){
		"json": func(conn *Connection) ([]byte, error) {
			return json.Marshal(conn)
		},
		"dsd-json": func(conn *Connection) ([]byte, error) {
			return dsd.Dump(conn, dsd.JSON)
		},
		"dsd-cbor": func(conn *Connection) ([]byte, error) {
			return dsd.Dump(conn, dsd.CBOR)
		},
		"dsd-msgpack": func(conn *Connection) ([]byte, error) {
			return dsd.Dump(conn, dsd.MsgPack)
		},
	}

	for name, conn := range serializationTestConnections() {
		for format, dump := range formats {
			// Serialize while holding the lock and after a lock cycle, like
			// all users of the connection do.
			conn.Lock()
			conn.Unlock() //nolint:staticcheck // Empty critical section is intended.
			conn.Lock()
			data, err := dump(conn)
			conn.Unlock()
			if err != nil {
				t.Fatalf("%s/%s: failed to serialize: %s", name, format, err)
			}

			goldenFile := filepath.Join("goldenfiles", "connection-"+name+"."+format+".golden")
			encoded := []byte(hex.EncodeToString(data) + "\n")
			if format == "json" {
				// Keep one human readable representation.
				encoded = []byte(string(data) + "\n")
			}

			if *updateGolden {
				err = os.WriteFile(goldenFile, encoded, 0o644) //nolint:gosec // Test data.
				if err != nil {
					t.Fatal(err)
				}
				continue
			}

			golden, err := os.ReadFile(goldenFile) //nolint:gosec // Test data.
			if err != nil {
				t.Fatalf("%s/%s: failed to read golden file: %s", name, format, err)
			}
			if !bytes.Equal(golden, encoded) {
				t.Errorf("%s/%s: serialization changed:\n got: %s\nwant: %s", name, format, encoded, golden)
			}
		}
	}
}
