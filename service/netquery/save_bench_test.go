package netquery

import (
	"context"
	"encoding/json"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/safing/portmaster/service/netquery/orm"
	"github.com/safing/portmaster/service/network"
	"github.com/safing/portmaster/service/network/netutils"
)

// benchSkipFields are the fields Database.Save does not encode.
var benchSkipFields = []string{"bytes_received", "bytes_sent"}

// newBenchConn returns a fully populated connection model, as produced by
// convertConnection for an ended outbound connection.
func newBenchConn() Conn {
	ended := time.Unix(1700000100, 0)
	allowed := true
	exitNode := "exit-node-id"
	return Conn{
		ID:              "1234-1700000000-6-192.168.1.10-51234-93.184.216.34-443",
		ProfileID:       "local/firefox",
		Path:            "/usr/lib/firefox/firefox",
		Type:            "ip",
		IPVersion:       4,
		IPProtocol:      6,
		LocalIP:         "192.168.1.10",
		LocalPort:       51234,
		RemoteIP:        "93.184.216.34",
		RemotePort:      443,
		Domain:          "www.example.com.",
		Country:         "US",
		ASN:             15133,
		ASOwner:         "Edgecast Inc.",
		Latitude:        42.1,
		Longitude:       -71.2,
		Scope:           netutils.Global,
		WorstVerdict:    network.VerdictAccept,
		ActiveVerdict:   network.VerdictAccept,
		FirewallVerdict: network.VerdictAccept,
		Started:         time.Unix(1700000000, 0),
		Ended:           &ended,
		Encrypted:       true,
		Direction:       "outbound",
		ExtraData:       json.RawMessage(`{"pid":1234,"processCreatedAt":1700000000,"cname":["e1.example.net."],"reason":{"Msg":"default permit"}}`),
		Allowed:         &allowed,
		ProfileRevision: 3,
		ExitNode:        &exitNode,
		ProfileName:     "Firefox",
	}
}

// BenchmarkToParamMapConn measures encoding a connection model into the
// parameter map that Database.Save uses for every saved connection.
func BenchmarkToParamMapConn(b *testing.B) {
	ctx := context.Background()
	conn := newBenchConn()

	b.ReportAllocs()
	for b.Loop() {
		_, err := orm.ToParamMap(ctx, conn, "", orm.DefaultEncodeConfig, benchSkipFields)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkSave measures Database.Save for a connection that is first inserted
// and then updated, without the history database.
func BenchmarkSave(b *testing.B) {
	db := newTestDatabase(b)
	ctx := context.Background()
	conn := newBenchConn()

	var i int
	b.ReportAllocs()
	for b.Loop() {
		// Insert a new connection on every second iteration and update it on
		// the other one.
		conn.ID = "bench-" + strconv.Itoa(i/2)
		i++
		err := db.Save(ctx, conn, false)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func TestToParamMapConnStable(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn := newBenchConn()

	m, err := orm.ToParamMap(ctx, conn, "", orm.DefaultEncodeConfig, benchSkipFields)
	require.NoError(t, err)
	require.Len(t, m, 33)
	require.NotContains(t, m, "bytes_received")
	require.Equal(t, "2023-11-14 22:13:20", m["started"])
	require.Equal(t, "2023-11-14 22:15:00", m["ended"])
	require.Equal(t, true, m["allowed"])
	require.Equal(t, "exit-node-id", m["exit_node"])

	// Nil pointers and the zero time are stored as NULL.
	conn.Ended = nil
	conn.Allowed = nil
	conn.ExitNode = nil
	conn.Started = time.Time{}
	m, err = orm.ToParamMap(ctx, conn, "", orm.DefaultEncodeConfig, benchSkipFields)
	require.NoError(t, err)
	require.Len(t, m, 33)
	for _, key := range []string{"ended", "allowed", "exit_node", "started"} {
		v, ok := m[key]
		require.True(t, ok, key)
		require.Nil(t, v, key)
	}
}

func TestSaveQuery(t *testing.T) {
	t.Parallel()

	q := newSaveQuery(map[string]any{"id": 1, "b": 2, "a": 3})
	require.Equal(t, []string{"a", "b", "id"}, q.columns)
	require.Equal(t, []string{":a", ":b", ":id"}, q.params)
	require.Equal(t, `INSERT INTO main.connections (a, b, id)
				VALUES(:a, :b, :id)
				ON CONFLICT(id) DO UPDATE SET
				a = :a, b = :b, id = :id
			`, q.sql(LiveDatabase))
	require.Equal(t, `INSERT INTO history.connections (a, b, id)
				VALUES(:a, :b, :id)
				ON CONFLICT(id) DO UPDATE SET
				a = :a, b = :b, id = :id
			`, q.sql(HistoryDatabase))
	require.Equal(t, `INSERT INTO other.connections (a, b, id)
				VALUES(:a, :b, :id)
				ON CONFLICT(id) DO UPDATE SET
				a = :a, b = :b, id = :id
			`, q.sql("other"))

	require.True(t, q.matches(map[string]any{"a": nil, "b": nil, "id": nil}))
	require.False(t, q.matches(map[string]any{"a": nil, "b": nil}))
	require.False(t, q.matches(map[string]any{"a": nil, "b": nil, "c": nil}))
	require.False(t, q.matches(map[string]any{"a": nil, "b": nil, "id": nil, "c": nil}))
	require.False(t, q.matches(map[string]any{}))
}

func TestSaveInsertAndUpdate(t *testing.T) { //nolint:paralleltest // Uses the package-level module.
	db := newTestDatabase(t)
	ctx := context.Background()

	conn := newBenchConn()
	conn.ID = "save-test"
	require.NoError(t, db.Save(ctx, conn, true))
	conn.Domain = "updated.example.com."
	conn.Ended = nil
	conn.Active = true
	require.NoError(t, db.Save(ctx, conn, true))

	for _, dbName := range []string{"main", "history"} {
		assert.Equal(t, 1, countRows(t, db, "SELECT COUNT(*) AS count FROM "+dbName+".connections WHERE id = 'save-test' AND domain = 'updated.example.com.' AND ended IS NULL AND active = 1"), dbName)
	}

	// Without history, only the live database is updated.
	conn.Domain = "live.example.com."
	require.NoError(t, db.Save(ctx, conn, false))
	assert.Equal(t, 1, countRows(t, db, "SELECT COUNT(*) AS count FROM main.connections WHERE domain = 'live.example.com.'"))
	assert.Equal(t, 0, countRows(t, db, "SELECT COUNT(*) AS count FROM history.connections WHERE domain = 'live.example.com.'"))
}
