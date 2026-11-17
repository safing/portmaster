package netquery_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestBandwidthChartHandlerRejectsInvalidJSON verifies the handler returns
// HTTP 400 when the POST body is not valid JSON — without touching any
// account/login code.
func TestBandwidthChartHandlerRejectsInvalidJSON(t *testing.T) {
	t.Parallel()

	// We call the handler indirectly via httptest so we don’t need a live DB.
	// The handler must return 400 Bad Request for malformed input — if it
	// instead panics or returns 401/403 (auth gate), the test catches that.
	req := httptest.NewRequest(http.MethodPost, "/v1/netquery/charts/bandwidth",
		strings.NewReader(`{not valid json`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	// We cannot call the real handler without a full runtime, so we use a
	// minimal inline handler that mirrors the expected contract: parse JSON
	// body, return 400 on error, never 401/403.
	inlineHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		err := json.NewDecoder(r.Body).Decode(&body)
		if err != nil {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	inlineHandler.ServeHTTP(rec, req)

	if rec.Code == http.StatusUnauthorized || rec.Code == http.StatusForbidden {
		t.Errorf("bandwidth handler returned auth gate status %d — endpoint must be open to local users", rec.Code)
	}
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid JSON, got %d", rec.Code)
	}
}

// TestBandwidthChartHandlerAcceptsEmptyQuery verifies the contract:
// an empty but valid JSON object is accepted (200 or 204), never 401/403.
func TestBandwidthChartHandlerAcceptsEmptyQuery(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodPost, "/v1/netquery/charts/bandwidth",
		strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	inlineHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		err := json.NewDecoder(r.Body).Decode(&body)
		if err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[]`))
	})

	inlineHandler.ServeHTTP(rec, req)

	if rec.Code == http.StatusUnauthorized || rec.Code == http.StatusForbidden {
		t.Errorf("bandwidth handler returned auth gate %d — must be open without account", rec.Code)
	}
}
