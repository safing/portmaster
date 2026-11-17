package netquery

import (
	"encoding/json"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/safing/portmaster/service/intel"
	"github.com/safing/portmaster/service/network"
	"github.com/safing/portmaster/service/resolver"
)

// oldExtraData is the previous implementation of the extra data of
// convertConnection, kept to check that the encoded extra data is unchanged.
func oldExtraData(conn *network.Connection) ([]byte, error) {
	extraData := map[string]any{
		"pid":              conn.ProcessContext.PID,
		"processCreatedAt": conn.ProcessContext.CreatedAt,
	}

	if conn.TunnelContext != nil {
		extraData["tunnel"] = conn.TunnelContext
	}

	if conn.SplitTunContext != nil {
		extraData["split_tun"] = conn.SplitTunContext
	}

	if conn.DNSContext != nil {
		extraData["dns"] = conn.DNSContext
	}

	if conn.Entity != nil {
		extraData["cname"] = conn.Entity.CNAME
		extraData["blockedByLists"] = conn.Entity.BlockedByLists
		extraData["blockedEntities"] = conn.Entity.BlockedEntities
		extraData["reason"] = conn.Reason
	}

	return json.Marshal(extraData)
}

// testTunnel is a tunnel context with exported fields.
type testTunnel struct {
	ExitNode string
	Hops     []string
	Since    time.Time
}

func (t testTunnel) GetExitNodeID() string { return t.ExitNode }
func (t testTunnel) StopTunnel() error     { return nil }

// testPtrTunnel is a tunnel context with methods with a pointer receiver.
type testPtrTunnel struct {
	ID string `json:"id"`
}

func (t *testPtrTunnel) GetExitNodeID() string {
	if t == nil {
		return ""
	}
	return t.ID
}
func (t *testPtrTunnel) StopTunnel() error { return nil }

// testMarshaler has a MarshalJSON method with a pointer receiver, which is
// only used if the value is addressable.
type testMarshaler struct {
	V string
}

func (m *testMarshaler) MarshalJSON() ([]byte, error) {
	return json.Marshal("marshaler:" + m.V)
}

// TestConvertConnectionExtraData checks that the extra data of
// convertConnection is encoded exactly like the previous map based
// implementation.
func TestConvertConnectionExtraData(t *testing.T) {
	t.Parallel()

	tunnels := []func(*network.Connection){
		func(c *network.Connection) { c.TunnelContext = nil },
		func(c *network.Connection) {
			c.TunnelContext = testTunnel{ExitNode: "exit<1>&", Hops: []string{"a", "b"}, Since: time.Unix(1700000000, 5).UTC()}
		},
		func(c *network.Connection) { c.TunnelContext = testTunnel{} },
		func(c *network.Connection) { c.TunnelContext = &testPtrTunnel{ID: "ptr"} },
		func(c *network.Connection) { c.TunnelContext = (*testPtrTunnel)(nil) },
	}
	splitTuns := []*network.SplitTunContext{
		nil,
		{},
		{Interface: "wg0", IP: net.ParseIP("10.0.0.2")},
		{Interface: "eth <0>", IP: net.ParseIP("fd00::1")},
	}
	dnsContexts := []*resolver.DNSRequestContext{
		nil,
		{},
		{
			Domain: "www.example.com.", Question: "AAAA", RCode: "NXDOMAIN",
			ServedFromCache: true, Filtered: true,
			Modified: time.Unix(1700000000, 0).UTC(), Expires: time.Unix(1700003600, 123).In(time.FixedZone("x", 3600)),
		},
	}
	entities := []func() *intel.Entity{
		func() *intel.Entity { return nil },
		func() *intel.Entity { return &intel.Entity{} },
		func() *intel.Entity {
			return &intel.Entity{CNAME: []string{}, BlockedByLists: []string{}, BlockedEntities: []string{}}
		},
		func() *intel.Entity {
			return &intel.Entity{
				CNAME:           []string{"e1.example.net.", "a<b>&c.example.", "\xff\xfe invalid"},
				BlockedByLists:  []string{"TRAC-1", "MAL-2"},
				BlockedEntities: []string{"example.com.", "\"quoted\""},
			}
		},
	}
	reasons := []network.Reason{
		{},
		{Msg: "allowed by default action", OptionKey: "filter/defaultAction", Profile: "local/browser"},
		{Msg: "<blocked> & \"quoted\"", Context: map[string]any{"z": 1, "a": []int{1, 2}, "m": nil}},
		{Msg: "struct", Context: testMarshaler{V: "value"}},
		{Msg: "pointer", Context: &testMarshaler{V: "pointer"}},
		{Msg: "list block", Context: intel.ListBlockReason{{Entity: "example.com.", ActiveLists: []string{"MAL-1"}}}},
		{Msg: "nil pointer", Context: (*testMarshaler)(nil)},
	}
	processes := []network.ProcessContext{
		{},
		{PID: 1234, CreatedAt: 1700000000123},
		{PID: -1, CreatedAt: -9007199254740993},
	}

	var cases int
	for _, setTunnel := range tunnels {
		for _, splitTun := range splitTuns {
			for _, dnsContext := range dnsContexts {
				for _, newEntity := range entities {
					for _, reason := range reasons {
						for _, proc := range processes {
							conn := newBenchNetworkConn(t, "extra-data")
							setTunnel(conn)
							conn.SplitTunContext = splitTun
							conn.DNSContext = dnsContext
							conn.Entity = newEntity()
							conn.Reason = reason
							conn.ProcessContext = proc

							want, wantErr := oldExtraData(conn)
							got, gotErr := convertConnection(conn)
							name := fmt.Sprintf("case %d", cases)
							if wantErr != nil {
								require.Error(t, gotErr, name)
								require.ErrorContains(t, gotErr, wantErr.Error(), name)
							} else {
								require.NoError(t, gotErr, name)
								require.Equal(t, string(want), string(got.ExtraData), name)
							}
							cases++
						}
					}
				}
			}
		}
	}

	// Values that cannot be encoded fail like before.
	conn := newBenchNetworkConn(t, "extra-data")
	conn.Reason.Context = make(chan int)
	_, wantErr := oldExtraData(conn)
	require.Error(t, wantErr)
	_, gotErr := convertConnection(conn)
	require.ErrorContains(t, gotErr, wantErr.Error())
}
