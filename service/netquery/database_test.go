package netquery

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/puddle/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitex"

	"github.com/safing/portmaster/base/utils"
	"github.com/safing/portmaster/service/netquery/orm"
	"github.com/safing/portmaster/service/profile"
)

// testInstance is the minimal module instance New() needs: the data directory
// that holds the history database.
type testInstance struct {
	dataDir string
}

func (i testInstance) DataDir() string                 { return i.dataDir }
func (i testInstance) Profile() *profile.ProfileModule { return nil }

// newTestDatabase creates the netquery database the same way the module does
// at startup, with the history database file in a temporary directory.
func newTestDatabase(tb testing.TB) *Database {
	tb.Helper()

	dataDir := tb.TempDir()
	module = &NetQuery{instance: testInstance{dataDir: dataDir}}
	tb.Cleanup(func() { module = nil })

	db, err := New(InMemory)
	require.NoError(tb, err)
	tb.Cleanup(func() { _ = db.Close() })

	// New() restricts the databases directory to administrators. Re-open it,
	// so that the history database can be created without elevation.
	require.NoError(tb, utils.SetFilePermission(filepath.Join(dataDir, "databases"), utils.PublicWriteExecPermission))

	require.NoError(tb, db.ApplyMigrations())

	return db
}

// acquirePoolConnections takes n connections from the read pool and releases
// them again. Every pooled connection attaches the history database, which is
// where the settings of the write connection get overridden.
func acquirePoolConnections(tb testing.TB, db *Database, n int) []*sqlite.Conn {
	tb.Helper()

	resources := make([]*puddle.Resource[*sqlite.Conn], 0, n)
	conns := make([]*sqlite.Conn, 0, n)
	for range n {
		res, err := db.readConnPool.Acquire(context.Background())
		require.NoError(tb, err)
		resources = append(resources, res)
		conns = append(conns, res.Value())
	}
	for _, res := range resources {
		res.Release()
	}

	return conns
}

func TestHistoryDatabaseJournalSettings(t *testing.T) { //nolint:paralleltest // uses the package-level module.
	db := newTestDatabase(t)

	// The write connection must have switched the history database to the
	// write-ahead log without fsync per commit.
	assert.Equal(t, "wal", pragma(t, db.writeConn, "PRAGMA history.journal_mode"), "history journal mode on the write connection")
	assert.Equal(t, "1", pragma(t, db.writeConn, "PRAGMA history.synchronous"), "history synchronous on the write connection (1 = NORMAL)")

	// Every pooled read connection attaches the history database again and
	// must apply the same synchronous level, as all connections share the
	// pager of the history database.
	for _, conn := range acquirePoolConnections(t, db, 3) {
		assert.Equal(t, "1", pragma(t, conn, "PRAGMA history.synchronous"), "history synchronous on a pooled read connection (1 = NORMAL)")
	}
}

func TestCleanupRemovesBandwidthOfDeletedConnections(t *testing.T) { //nolint:paralleltest // uses the package-level module.
	db := newTestDatabase(t)
	ctx := context.Background()

	// The index the bandwidth chart and the cleanup rely on must exist.
	assert.Equal(t, 1, countRows(t, db, "SELECT COUNT(*) AS count FROM main.sqlite_master WHERE type = 'index' AND name = 'bandwidth_conn_id_index'"), "bandwidth index")

	const processKey = "test-process"
	now := time.Now()
	endedLongAgo := now.Add(-time.Hour)

	// One connection that ended long ago, one that is still active.
	ended := Conn{ID: makeNqIDFromParts(processKey, "ended"), ProfileID: "local/test", Started: now.Add(-2 * time.Hour), Ended: &endedLongAgo}
	active := Conn{ID: makeNqIDFromParts(processKey, "active"), ProfileID: "local/test", Started: now, Active: true}
	require.NoError(t, db.Save(ctx, ended, false))
	require.NoError(t, db.Save(ctx, active, false))

	// Two bandwidth rows each, plus one row of a connection that the feed has
	// not saved yet, which happens when a bandwidth update overtakes the save.
	for range 2 {
		require.NoError(t, db.UpdateBandwidth(ctx, false, ended.ProfileID, processKey, "ended", 10, 20))
		require.NoError(t, db.UpdateBandwidth(ctx, false, active.ProfileID, processKey, "active", 10, 20))
	}
	require.NoError(t, db.UpdateBandwidth(ctx, false, "local/test", processKey, "unsaved", 10, 20))
	unsavedID := makeNqIDFromParts(processKey, "unsaved")
	require.Equal(t, 5, countRows(t, db, "SELECT COUNT(*) AS count FROM main.bandwidth"), "bandwidth rows before cleanup")

	// Cleanup removes the ended connection and must remove its bandwidth rows,
	// and only those.
	removed, err := db.Cleanup(ctx, now)
	require.NoError(t, err)
	assert.Equal(t, 1, removed, "removed connections")
	assert.Equal(t, 0, countRows(t, db, "SELECT COUNT(*) AS count FROM main.bandwidth WHERE conn_id = '"+ended.ID+"'"), "bandwidth rows of the deleted connection")
	assert.Equal(t, 2, countRows(t, db, "SELECT COUNT(*) AS count FROM main.bandwidth WHERE conn_id = '"+active.ID+"'"), "bandwidth rows of the active connection")
	assert.Equal(t, 1, countRows(t, db, "SELECT COUNT(*) AS count FROM main.bandwidth WHERE conn_id = '"+unsavedID+"'"), "bandwidth rows of a connection that is not saved yet")
}

// countRows runs a query that returns a single "count" column and returns its value.
func countRows(tb testing.TB, db *Database, query string) int {
	tb.Helper()

	var result []struct {
		Count int `sqlite:"count"`
	}
	require.NoError(tb, db.Execute(context.Background(), query, orm.WithResult(&result)), query)
	require.Len(tb, result, 1, query)

	return result[0].Count
}

// BenchmarkHistoryWrite measures one write to the history database after the
// read pool has been used, which is the state during normal operation.
// Run it before and after a change to compare:
//
//	go test ./service/netquery/ -run '^$' -bench 'BenchmarkHistory|BenchmarkUpdateBandwidth' -benchtime 200x
func BenchmarkHistoryWrite(b *testing.B) {
	db := newTestDatabase(b)
	acquirePoolConnections(b, db, 3)

	ctx := context.Background()
	require.NoError(b, db.ExecuteWrite(ctx, "CREATE TABLE history.bench (id INTEGER PRIMARY KEY, n INTEGER)"))
	require.NoError(b, db.ExecuteWrite(ctx, "INSERT INTO history.bench (id, n) VALUES (1, 0)"))

	for b.Loop() {
		if err := db.ExecuteWrite(ctx, "UPDATE history.bench SET n = n + 1 WHERE id = 1"); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkUpdateBandwidthWithHistory measures the production path that runs
// once per second for every connection with bandwidth and history enabled.
func BenchmarkUpdateBandwidthWithHistory(b *testing.B) {
	db := newTestDatabase(b)
	acquirePoolConnections(b, db, 3)

	const (
		profileKey = "local/bench"
		processKey = "bench-process"
		connID     = "bench-conn"
	)
	ctx := context.Background()
	require.NoError(b, db.Save(ctx, Conn{
		ID:        makeNqIDFromParts(processKey, connID),
		ProfileID: profileKey,
		Started:   time.Now(),
	}, true))

	for b.Loop() {
		if err := db.UpdateBandwidth(ctx, true, profileKey, processKey, connID, 100, 50); err != nil {
			b.Fatal(err)
		}
	}
}

// pragma runs a PRAGMA query on conn and returns the first column of its result.
func pragma(tb testing.TB, conn *sqlite.Conn, query string) string {
	tb.Helper()

	var result string
	err := sqlitex.ExecuteTransient(conn, query, &sqlitex.ExecOptions{
		ResultFunc: func(stmt *sqlite.Stmt) error {
			result = stmt.ColumnText(0)
			return nil
		},
	})
	require.NoError(tb, err, query)

	return result
}
