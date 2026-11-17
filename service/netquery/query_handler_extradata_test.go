package netquery

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestQueryHandlerEmptyExtraData checks that a connection with empty extra
// data is returned as a valid JSON response.
func TestQueryHandlerEmptyExtraData(t *testing.T) {
	t.Parallel()

	db := newTestDatabase(t)
	conn := newBenchConn()
	conn.ID = "empty-extra-data"
	conn.ExtraData = json.RawMessage{}
	require.NoError(t, db.Save(context.Background(), conn, false))

	handler := &QueryHandler{
		IsDevMode: func() bool { return false },
		Database:  db,
	}
	query := `{"query":{"id":"empty-extra-data"},"pageSize":10}`
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/query?q="+url.QueryEscape(query), nil))

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var body struct {
		Results []map[string]json.RawMessage `json:"results"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body), "response is not valid JSON: %q", rec.Body.String())
	require.Len(t, body.Results, 1)
	require.Equal(t, "null", string(body.Results[0]["extra_data"]))
}
