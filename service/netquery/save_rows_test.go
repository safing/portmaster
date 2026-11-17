package netquery

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitex"

	"github.com/safing/portmaster/service/netquery/orm"
	"github.com/safing/portmaster/service/network"
)

// newReferenceSaveQuery builds the statements Database.Save used before the
// insert-only columns were left out of the update: an update sets every
// column.
func newReferenceSaveQuery(tb testing.TB) *saveQuery {
	tb.Helper()

	connMap, err := orm.ToParamMap(context.Background(), newBenchConn(), "", orm.DefaultEncodeConfig, benchSkipFields)
	require.NoError(tb, err)

	q := newSaveQuery(connMap)
	updateSets := make([]string, 0, len(q.columns))
	for _, column := range q.columns {
		updateSets = append(updateSets, fmt.Sprintf("%s = excluded.%s", column, column))
	}
	require.Greater(tb, len(updateSets), strings.Count(q.updateSetList, ",")+1, "no insert-only columns")
	q.updateSetList = strings.Join(updateSets, ", ")
	q.liveSQL = q.buildSQL(LiveDatabase)
	q.historySQL = q.buildSQL(HistoryDatabase)
	return q
}

// dumpConnections returns all columns of all rows of the connections table.
func dumpConnections(tb testing.TB, db *Database, dbName DatabaseName) []string {
	tb.Helper()

	var rows []string
	db.l.Lock()
	defer db.l.Unlock()
	// All columns are compared, whatever the schema is.
	sql := fmt.Sprintf("SELECT * FROM %s.connections ORDER BY id", dbName) //nolint:unqueryvet
	err := sqlitex.Execute(db.writeConn, sql, &sqlitex.ExecOptions{
		ResultFunc: func(stmt *sqlite.Stmt) error {
			var row strings.Builder
			for i := range stmt.ColumnCount() {
				fmt.Fprintf(&row, "%s=%s:%q ", stmt.ColumnName(i), stmt.ColumnType(i), stmt.ColumnText(i))
			}
			rows = append(rows, row.String())
			return nil
		},
	})
	require.NoError(tb, err)
	return rows
}

// TestSaveStoresSameRowsAsFullUpdate checks that leaving the insert-only
// columns out of the update stores exactly the rows that setting every
// column stored, for inserts and for updates that change every other column.
func TestSaveStoresSameRowsAsFullUpdate(t *testing.T) { //nolint:paralleltest // uses the package-level module.
	ctx := context.Background()

	// saveAll saves a typical series of connection updates.
	saveAll := func(db *Database) {
		for i := range 20 {
			history := i%2 == 0

			// After the decision.
			conn := newBenchConn()
			conn.ID = "conn-" + strconv.Itoa(i)
			conn.Started = time.Unix(1700000000+int64(i), 0)
			conn.Ended = nil
			conn.Active = true
			allowed := false
			conn.Allowed = &allowed
			conn.ExitNode = nil
			require.NoError(t, db.Save(ctx, conn, history))

			// Bandwidth is updated independently and must survive updates.
			require.NoError(t, db.UpdateBandwidth(ctx, history, conn.ProfileID, "", conn.ID, uint64(100*i), uint64(10*i)))

			if i%5 == 4 {
				// Only saved once.
				continue
			}

			// Update that changes every column but the ID and the start.
			ended := time.Unix(1700000100+int64(i), 0)
			established := true
			exitNode := "exit-" + strconv.Itoa(i)
			conn.ProfileID = "local/other-" + strconv.Itoa(i)
			conn.Path = "/usr/bin/other"
			conn.Type = "dns"
			conn.External = true
			conn.IPVersion = 6
			conn.IPProtocol = 17
			conn.LocalIP = "fd00::1"
			conn.LocalPort = 1000 + uint16(i) //nolint:gosec // Small test value.
			conn.RemoteIP = "2001:db8::1"
			conn.RemotePort = 53
			conn.Domain = "other.example.org."
			conn.Country = "AT"
			conn.ASN = 1234
			conn.ASOwner = "Other"
			conn.Latitude = 48.2
			conn.Longitude = 16.4
			conn.Scope++
			conn.WorstVerdict = network.VerdictBlock
			conn.ActiveVerdict = network.VerdictBlock
			conn.FirewallVerdict = network.VerdictBlock
			conn.Ended = &ended
			conn.Active = false
			conn.Tunneled = true
			conn.Encrypted = false
			conn.Internal = true
			conn.Direction = "inbound"
			conn.ExtraData = json.RawMessage(`{"pid":4321}`)
			conn.Allowed = &established
			conn.ProfileRevision = 7
			conn.ExitNode = &exitNode
			conn.ProfileName = "Other"
			require.NoError(t, db.Save(ctx, conn, history))
		}
	}

	db := newTestDatabase(t)
	saveAll(db)
	live, history := dumpConnections(t, db, LiveDatabase), dumpConnections(t, db, HistoryDatabase)
	_ = db.Close()

	reference := newTestDatabase(t)
	reference.saveQuery.Store(newReferenceSaveQuery(t))
	saveAll(reference)
	require.Contains(t, reference.saveQuery.Load().updateSetList, "started = excluded.started", "reference statement was replaced")

	require.Len(t, live, 20)
	require.Len(t, history, 10)
	assert.Equal(t, dumpConnections(t, reference, LiveDatabase), live)
	assert.Equal(t, dumpConnections(t, reference, HistoryDatabase), history)
}
