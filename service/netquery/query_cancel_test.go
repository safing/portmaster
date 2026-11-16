package netquery

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/safing/portmaster/service/netquery/orm"
)

// slowQuery counts to a huge number with a recursive CTE. Uninterrupted it
// keeps a pooled read connection busy for minutes.
const slowQuery = `WITH RECURSIVE c(x) AS (SELECT 1 UNION ALL SELECT x+1 FROM c WHERE x < :limit) SELECT count(*) AS n FROM c`

// TestExecuteInterruptedByContext verifies that cancelling the (request)
// context passed to Execute and ExecuteBatch, as the HTTP query handlers do,
// aborts the running statement and returns the connection to the pool in a
// usable state.
func TestExecuteInterruptedByContext(t *testing.T) { //nolint:paralleltest // uses the package-level module.
	db := newTestDatabase(t)
	slowArgs := map[string]any{":limit": int64(5_000_000_000)}

	run := func(name string, exec func(ctx context.Context) error) {
		t.Helper()

		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()

		start := time.Now()
		err := exec(ctx)
		elapsed := time.Since(start)

		require.Error(t, err, name)
		assert.ErrorIs(t, err, context.DeadlineExceeded, name)
		assert.Less(t, elapsed, 2*time.Second, "%s: query was not interrupted by the context", name)
		t.Logf("%s returned after %s: %s", name, elapsed, err)
	}

	// Run more cancelled queries than the pool has connections, so that
	// interrupted connections are reused.
	for range 12 {
		run("Execute", func(ctx context.Context) error {
			var result []map[string]any
			return db.Execute(ctx, slowQuery, orm.WithNamedArgs(slowArgs), orm.WithResult(&result))
		})
	}
	run("ExecuteBatch", func(ctx context.Context) error {
		var result []map[string]any
		return db.ExecuteBatch(ctx, []BatchExecute{{ID: "slow", SQL: slowQuery, Params: slowArgs, Result: &result}})
	})

	// Every pooled connection, including the interrupted ones, must still
	// work, including the cached, previously interrupted statement.
	for _, conn := range acquirePoolConnections(t, db, 10) {
		assert.Equal(t, "1", pragma(t, conn, "SELECT 1"))
	}
	for range 12 {
		var result []struct {
			N int `sqlite:"n"`
		}
		require.NoError(t, db.Execute(context.Background(), slowQuery, orm.WithNamedArgs(map[string]any{":limit": int64(1000)}), orm.WithResult(&result)))
		require.Len(t, result, 1)
		assert.Equal(t, 1000, result[0].N)
	}
}
