package netquery

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitex"

	"github.com/safing/portmaster/service/netquery/orm"
)

func newPagedTestRequest(page, pageSize int) *QueryRequestPayload {
	return &QueryRequestPayload{
		Select:     Selects{{Field: "id"}},
		Query:      Query{"profile": []Matcher{{Equal: "test/profile"}}},
		OrderBy:    OrderBys{{Field: "id"}},
		Pagination: Pagination{Page: page, PageSize: pageSize},
	}
}

// TestPaginationSQLIsStable verifies that LIMIT and OFFSET are bound as
// parameters so that the generated SQL text (which is the key of the
// per-connection prepared statement cache) does not depend on the
// requested page or page size.
func TestPaginationSQLIsStable(t *testing.T) {
	t.Parallel()

	schema, err := orm.GenerateTableSchema("connections", Conn{})
	require.NoError(t, err)

	cases := []struct {
		page, pageSize      int
		wantLimit, wantOffs int
	}{
		{0, 0, 100, 0},      // default page size
		{0, 25, 25, 0},      // first page
		{3, 25, 25, 75},     // later page
		{2, 1000, 100, 200}, // page size capped at 100
		{-1, 10, 10, 0},     // negative page -> no offset
		{7, -5, 100, 700},   // negative page size -> default
	}

	distinctSQL := make(map[string]struct{})
	for _, tc := range cases {
		sql, params, err := newPagedTestRequest(tc.page, tc.pageSize).generateSQL(context.Background(), schema)
		require.NoError(t, err)
		distinctSQL[sql] = struct{}{}

		assert.Equal(t, tc.wantLimit, params[paginationLimitParam], "limit for page=%d size=%d", tc.page, tc.pageSize)
		assert.Equal(t, tc.wantOffs, params[paginationOffsetParam], "offset for page=%d size=%d", tc.page, tc.pageSize)
		assert.Equal(t, "test/profile", params[":profile0eq0"])
		assert.NotContains(t, sql, fmt.Sprintf("LIMIT %d", tc.wantLimit))
	}

	for page := range 50 {
		for size := 1; size <= 100; size++ {
			sql, _, err := newPagedTestRequest(page, size).generateSQL(context.Background(), schema)
			require.NoError(t, err)
			distinctSQL[sql] = struct{}{}
		}
	}

	assert.Len(t, distinctSQL, 1, "SQL text must not depend on pagination: %v", distinctSQL)
}

// cachedStmtCount returns the number of persistent prepared statements
// cached by conn (zombiezen's Conn.Prepare caches statements keyed by
// SQL text and never evicts them).
func cachedStmtCount(t *testing.T, conn *sqlite.Conn) int {
	t.Helper()

	field := reflect.ValueOf(conn).Elem().FieldByName("stmts")
	if !field.IsValid() || field.Kind() != reflect.Map {
		t.Skip("cannot inspect statement cache of sqlite.Conn")
	}

	return field.Len()
}

func openPagingTestConn(t *testing.T, rows int) (*sqlite.Conn, *orm.TableSchema) {
	t.Helper()

	schema, err := orm.GenerateTableSchema("connections", Conn{})
	require.NoError(t, err)

	conn, err := sqlite.OpenConn(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	require.NoError(t, sqlitex.ExecuteTransient(conn, schema.CreateStatement("main", false), nil))

	for i := range rows {
		profile := "test/profile"
		if i%3 == 0 {
			profile = "other/profile"
		}
		connMap, err := orm.ToParamMap(context.Background(), Conn{
			ID:        fmt.Sprintf("conn-%05d", i),
			ProfileID: profile,
			Type:      "ip",
			Started:   time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			ExtraData: json.RawMessage("{}"),
		}, "", orm.DefaultEncodeConfig, nil)
		require.NoError(t, err)

		columns := make([]string, 0, len(connMap))
		placeholders := make([]string, 0, len(connMap))
		values := make(map[string]any, len(connMap))
		for key, value := range connMap {
			columns = append(columns, key)
			placeholders = append(placeholders, ":"+key)
			values[":"+key] = value
		}

		require.NoError(t, sqlitex.ExecuteTransient(conn,
			"INSERT INTO main.connections ("+strings.Join(columns, ", ")+") VALUES ("+strings.Join(placeholders, ", ")+")",
			&sqlitex.ExecOptions{Named: values},
		))
	}

	return conn, schema
}

// TestPaginationEndToEnd runs paged queries against an in-memory database
// and verifies that results are correct and that paging through the
// results does not grow the per-connection prepared statement cache.
func TestPaginationEndToEnd(t *testing.T) {
	t.Parallel()

	const totalRows = 300
	conn, schema := openPagingTestConn(t, totalRows)

	var want []string
	for i := range totalRows {
		if i%3 != 0 {
			want = append(want, fmt.Sprintf("conn-%05d", i))
		}
	}

	type row struct {
		ID string `sqlite:"id"`
	}

	runPage := func(page, size int) []string {
		sql, params, err := newPagedTestRequest(page, size).generateSQL(context.Background(), schema)
		require.NoError(t, err)

		var result []row
		require.NoError(t, orm.RunQuery(context.Background(), conn, sql, orm.WithNamedArgs(params), orm.WithResult(&result)))

		ids := make([]string, 0, len(result))
		for _, r := range result {
			ids = append(ids, r.ID)
		}
		return ids
	}

	baseline := cachedStmtCount(t, conn)

	// Page through all results with various page sizes and verify the
	// concatenation of all pages equals the full, ordered result set.
	for _, size := range []int{1, 7, 25, 64, 100, 500} {
		var got []string
		for page := 0; ; page++ {
			ids := runPage(page, size)
			got = append(got, ids...)

			effective := min(size, 100)
			if len(ids) < effective {
				break
			}
		}
		assert.Equal(t, want, got, "page size %d", size)
	}

	// Default page size (0 -> 100) on the first page.
	assert.Equal(t, want[:100], runPage(0, 0))

	// Hundreds of different page/size combinations must only ever have
	// produced a single cached statement.
	assert.Equal(t, 1, cachedStmtCount(t, conn)-baseline, "paging must reuse one prepared statement")
}

// BenchmarkPagedQuery measures paging through results on a single
// connection, as the UI does when scrolling through the connection
// history, and reports the number of statements left in the cache.
func BenchmarkPagedQuery(b *testing.B) {
	schema, err := orm.GenerateTableSchema("connections", Conn{})
	require.NoError(b, err)

	conn, err := sqlite.OpenConn(":memory:")
	require.NoError(b, err)
	defer func() { _ = conn.Close() }()
	require.NoError(b, sqlitex.ExecuteTransient(conn, schema.CreateStatement("main", false), nil))

	type row struct {
		ID string `sqlite:"id"`
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; b.Loop(); i++ {
		sql, params, err := newPagedTestRequest(i, 25).generateSQL(context.Background(), schema)
		if err != nil {
			b.Fatal(err)
		}
		var result []row
		err = orm.RunQuery(context.Background(), conn, sql, orm.WithNamedArgs(params), orm.WithResult(&result))
		if err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()

	field := reflect.ValueOf(conn).Elem().FieldByName("stmts")
	if field.IsValid() && field.Kind() == reflect.Map {
		b.ReportMetric(float64(field.Len()), "cached-stmts")
	}
}
