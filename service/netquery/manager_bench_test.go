package netquery

import (
	"context"
	"net"
	"reflect"
	"strconv"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"
	"github.com/tevino/abool"

	"github.com/safing/portmaster/base/database/record"
	"github.com/safing/portmaster/base/runtime"
	"github.com/safing/portmaster/service/intel"
	"github.com/safing/portmaster/service/intel/geoip"
	"github.com/safing/portmaster/service/network"
	"github.com/safing/portmaster/service/network/netutils"
	"github.com/safing/portmaster/service/network/packet"
	"github.com/safing/portmaster/service/process"
	"github.com/safing/portmaster/service/resolver"
)

// setUnexportedField sets an unexported field of a struct from another
// package. Complete connections can otherwise only be created by the packet
// path, which needs the process and profile modules.
func setUnexportedField(tb testing.TB, structPtr any, name string, value any) {
	tb.Helper()

	field := reflect.ValueOf(structPtr).Elem().FieldByName(name)
	require.True(tb, field.IsValid(), name)
	ptr := unsafe.Pointer(field.UnsafeAddr()) //nolint:gosec // Test-only access to an unexported field.
	reflect.NewAt(field.Type(), ptr).Elem().Set(reflect.ValueOf(value))
}

// newBenchNetworkConn returns a complete outbound connection as it leaves the
// firewall after the decision: attributed to a process and profile, with a
// resolved domain and filter list results.
func newBenchNetworkConn(tb testing.TB, id string) *network.Connection {
	tb.Helper()

	conn := &network.Connection{
		ID:         id,
		Type:       network.IPConnection,
		IPVersion:  packet.IPv4,
		IPProtocol: packet.TCP,
		LocalIP:    net.ParseIP("192.168.1.10"),
		LocalPort:  51234,
		PID:        1234,
		Started:    1700000000,
		Encrypted:  true,
		Entity: &intel.Entity{
			IP:              net.ParseIP("93.184.216.34"),
			IPScope:         netutils.Global,
			Protocol:        6,
			Port:            443,
			Domain:          benchDomain,
			CNAME:           []string{"e1.example.net.", "edge.example-cdn.net."},
			Country:         "US",
			Coordinates:     &geoip.Coordinates{AccuracyRadius: 100, Latitude: 42.1, Longitude: -71.2},
			ASN:             15133,
			ASOrg:           "Edgecast Inc.",
			BlockedByLists:  []string{"TRAC", "MAL"},
			BlockedEntities: []string{benchDomain},
		},
		Verdict: network.VerdictAccept,
		Reason: network.Reason{
			Msg:       "allowed by default action",
			OptionKey: "filter/defaultAction",
			Profile:   "local/firefox",
		},
		ProcessContext: network.ProcessContext{
			ProcessName: "firefox",
			ProfileName: "Firefox",
			BinaryPath:  "/usr/lib/firefox/firefox",
			CmdLine:     "/usr/lib/firefox/firefox --new-tab",
			PID:         1234,
			CreatedAt:   1700000000,
			Profile:     "firefox",
			Source:      "local",
		},
		DNSContext: &resolver.DNSRequestContext{
			Domain:   benchDomain,
			Question: "A",
			RCode:    "NOERROR",
		},
		ProfileRevisionCounter: 3,
		HistoryEnabled:         false,
	}
	setUnexportedField(tb, conn, "process", &process.Process{Pid: 1234})
	setUnexportedField(tb, conn, "dataComplete", abool.NewBool(true))
	conn.SetMeta(&record.Meta{})
	conn.UpdateMeta()

	return conn
}

func newBenchManager(tb testing.TB) (*Manager, *Database) {
	tb.Helper()

	db := newTestDatabase(tb)
	mng, err := NewManager(db, "netquery/conns/", runtime.NewRegistry())
	require.NoError(tb, err)
	return mng, db
}

// BenchmarkConvertConnection measures converting a connection to the netquery
// model, which is done for every saved connection update.
func BenchmarkConvertConnection(b *testing.B) {
	conn := newBenchNetworkConn(b, "bench")

	b.ReportAllocs()
	for b.Loop() {
		_, err := convertConnection(conn)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkHandleConnection measures everything netquery does for a connection
// update fed by the network module: converting the connection, saving it to
// the live database and pushing the update. Every connection is saved three
// times, like a typical connection: after the decision, after the first
// bandwidth/verdict update and when it ended.
func BenchmarkHandleConnection(b *testing.B) {
	for _, history := range []bool{false, true} {
		b.Run("history="+strconv.FormatBool(history), func(b *testing.B) {
			mng, _ := newBenchManager(b)
			ctx := context.Background()

			const saves = 3
			conns := make([]*network.Connection, 0, 256)
			for i := range cap(conns) {
				conn := newBenchNetworkConn(b, "bench-"+strconv.Itoa(i))
				conn.HistoryEnabled = history
				conns = append(conns, conn)
			}

			var i int
			b.ReportAllocs()
			for b.Loop() {
				conn := conns[(i/saves)%len(conns)]
				if i%saves == saves-1 {
					conn.Ended = 1700000100
				} else {
					conn.Ended = 0
				}
				i++
				mng.handleConnection(ctx, conn)
			}
		})
	}
}
