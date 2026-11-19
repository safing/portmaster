package updates

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestDownloadDataStall checks that a download that stops making progress is
// aborted instead of blocking the updater forever, and that a slow but
// steady download is not.
func TestDownloadDataStall(t *testing.T) { //nolint:paralleltest // Changes the stall timeout.
	old := downloadStallTimeout
	downloadStallTimeout = 200 * time.Millisecond
	t.Cleanup(func() { downloadStallTimeout = old })
	oldDelays := downloadRetryDelays
	downloadRetryDelays = nil // Check a single download.
	t.Cleanup(func() { downloadRetryDelays = oldDelays })

	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	mux := http.NewServeMux()
	mux.HandleFunc("/no-headers", func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	})
	mux.HandleFunc("/stalled-body", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("partial"))
		w.(http.Flusher).Flush() //nolint:forcetypeassert // Test server supports flushing.
		select {
		case <-release:
		case <-r.Context().Done():
		}
	})
	mux.HandleFunc("/slow-body", func(w http.ResponseWriter, _ *http.Request) {
		// 10 chunks with a pause of half the stall timeout: the whole
		// download takes longer than the stall timeout, but never stalls.
		for range 10 {
			_, _ = w.Write([]byte("chunk"))
			w.(http.Flusher).Flush() //nolint:forcetypeassert // Test server supports flushing.
			time.Sleep(downloadStallTimeout / 2)
		}
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	d := &Downloader{}
	for _, path := range []string{"/no-headers", "/stalled-body"} {
		start := time.Now()
		_, err := d.downloadData(t.Context(), server.URL+path)
		if !errors.Is(err, errDownloadStalled) {
			t.Errorf("%s: got error %v, want %v", path, err, errDownloadStalled)
		}
		if elapsed := time.Since(start); elapsed > 5*time.Second {
			t.Errorf("%s: download was aborted only after %s", path, elapsed)
		}
	}

	data, err := d.downloadData(t.Context(), server.URL+"/slow-body")
	if err != nil {
		t.Fatalf("slow but steady download failed: %s", err)
	}
	if len(data) != 50 {
		t.Errorf("got %d bytes, want 50", len(data))
	}
}
