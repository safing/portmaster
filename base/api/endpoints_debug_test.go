package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestReadyEndpoint checks that the ready endpoint reports success only once
// Portmaster has completed starting, and 425 Too Early before that.
func TestReadyEndpoint(t *testing.T) { //nolint:paralleltest // Modifies the shared test instance.
	stub, ok := module.instance.(*testInstance)
	if !ok {
		t.Fatalf("unexpected test instance type %T", module.instance)
	}
	defer stub.notReady.Store(false)

	testHandler := &mainHandler{
		mux: mainMux,
	}
	get := func() *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		testHandler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, apiV1Path+"ready", nil))
		return w
	}

	// Still starting.
	stub.notReady.Store(true)
	w := get()
	if w.Code != http.StatusTooEarly {
		t.Errorf("not ready: expected status %d, got %d: %s", http.StatusTooEarly, w.Code, strings.TrimSpace(w.Body.String()))
	}

	// Started.
	stub.notReady.Store(false)
	w = get()
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Portmaster is ready.") {
		t.Errorf("ready: expected status %d with ready message, got %d: %s", http.StatusOK, w.Code, strings.TrimSpace(w.Body.String()))
	}
}
