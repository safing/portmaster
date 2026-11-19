package updates

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// TestDownloadDataRetry checks that a download that fails midway is retried,
// and that a client error is not.
func TestDownloadDataRetry(t *testing.T) { //nolint:paralleltest // Changes the retry delays.
	old := downloadRetryDelays
	downloadRetryDelays = []time.Duration{10 * time.Millisecond, 10 * time.Millisecond}
	t.Cleanup(func() { downloadRetryDelays = old })

	var flakyCalls, missingCalls atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/flaky", func(w http.ResponseWriter, _ *http.Request) {
		if flakyCalls.Add(1) <= 2 {
			// Promise more data than is sent, then drop the connection.
			w.Header().Set("Content-Length", "100")
			_, _ = w.Write([]byte("partial"))
			panic(http.ErrAbortHandler)
		}
		_, _ = w.Write([]byte("complete"))
	})
	mux.HandleFunc("/missing", func(w http.ResponseWriter, _ *http.Request) {
		missingCalls.Add(1)
		http.NotFound(w, nil)
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	d := &Downloader{}
	data, err := d.downloadData(t.Context(), server.URL+"/flaky")
	if err != nil {
		t.Fatalf("download failed despite retries: %s", err)
	}
	if string(data) != "complete" || flakyCalls.Load() != 3 {
		t.Errorf("got %q after %d calls, want \"complete\" after 3", data, flakyCalls.Load())
	}

	_, err = d.downloadData(t.Context(), server.URL+"/missing")
	if err == nil {
		t.Fatal("download of a missing file succeeded")
	}
	if missingCalls.Load() != 1 {
		t.Errorf("a 404 was requested %d times, want 1", missingCalls.Load())
	}
}
