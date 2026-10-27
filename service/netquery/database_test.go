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
