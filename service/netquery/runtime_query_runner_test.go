package netquery

import (
	"context"
	"fmt"
	"testing"

	"github.com/jackc/puddle/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"zombiezen.com/go/sqlite"

	"github.com/safing/portmaster/base/runtime"
)

// TestRuntimeQueryRunnerDoesNotCacheStatements verifies that ad-hoc SQL
// queries received via the runtime database are not added to the statement
// cache of the pooled read connection.
func TestRuntimeQueryRunnerDoesNotCacheStatements(t *testing.T) {
	t.Parallel()

	pool, err := puddle.NewPool(&puddle.Config[*sqlite.Conn]{
		Constructor: func(context.Context) (*sqlite.Conn, error) {
			return sqlite.OpenConn(":memory:")
		},
		Destructor: func(c *sqlite.Conn) { _ = c.Close() },
		MaxSize:    1,
	})
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	runner := &RuntimeQueryRunner{
		db:        &Database{readConnPool: pool},
		reg:       runtime.NewRegistry(),
		keyPrefix: "netquery/query/",
	}

	for i := range 50 {
		records, err := runner.get(fmt.Sprintf("netquery/query/SELECT %d AS value", i))
		require.NoError(t, err)
		require.Len(t, records, 1)
	}

	res, err := pool.Acquire(context.Background())
	require.NoError(t, err)
	defer res.Release()

	assert.Equal(t, 0, cachedStmtCount(t, res.Value()), "ad-hoc runtime queries must not be cached")
}
