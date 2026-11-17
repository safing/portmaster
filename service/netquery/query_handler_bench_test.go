package netquery

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

// newQueryBenchDatabase returns a database with n fully populated
// connections in the live database.
func newQueryBenchDatabase(b *testing.B, n int) *Database {
	b.Helper()

	db := newTestDatabase(b)
	ctx := context.Background()
	conn := newBenchConn()
	for i := range n {
		conn.ID = "bench-" + strconv.Itoa(i)
		conn.RemotePort = uint16(i) //nolint:gosec // Benchmark data.
		require.NoError(b, db.Save(ctx, conn, false))
	}

	return db
}

func runQueryHandlerBench(b *testing.B, query string) {
	b.Helper()

	db := newQueryBenchDatabase(b, 1000)
	handler := &QueryHandler{
		IsDevMode: func() bool { return false },
		Database:  db,
	}
	target := "/query?q=" + url.QueryEscape(query)

	b.ReportAllocs()
	for b.Loop() {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
		if rec.Code != http.StatusOK {
			b.Fatalf("unexpected status %d: %s", rec.Code, rec.Body.String())
		}
	}
}

// BenchmarkQueryHandlerConnections measures the query API as used by the
// connection list of the UI: a full page (100 rows) of all columns, decoded
// into maps and JSON encoded.
func BenchmarkQueryHandlerConnections(b *testing.B) {
	runQueryHandlerBench(b, `{"query":{"profile":"local/firefox"},"orderBy":[{"field":"started","desc":true}],"pageSize":100}`)
}

// BenchmarkQueryHandlerGrouped measures an aggregated query as used by the
// UI to group connections by domain.
func BenchmarkQueryHandlerGrouped(b *testing.B) {
	runQueryHandlerBench(b, `{"select":["domain","remote_port",{"$count":{"field":"*","as":"totalCount"}}],"groupBy":["domain","remote_port"],"pageSize":100}`)
}
