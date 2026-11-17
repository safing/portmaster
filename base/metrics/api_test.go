package metrics

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	vm "github.com/VictoriaMetrics/metrics"
)

func TestWriteMetricsToErrorWithEndlessBody(t *testing.T) { //nolint:paralleltest // Modifies package globals.
	// Register a single test metric, so that there is something to push.
	set := vm.NewSet()
	set.NewCounter("push_test_total").Inc()
	registryLock.Lock()
	prevRegistry := registry
	registry = []Metric{&metricBase{
		Identifier:        "push_test_total",
		LabeledIdentifier: "push_test_total",
		Options:           &Options{},
		set:               set,
	}}
	registryLock.Unlock()
	t.Cleanup(func() {
		registryLock.Lock()
		registry = prevRegistry
		registryLock.Unlock()
	})

	// Server that responds with an error and a body that never ends.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		chunk := []byte(strings.Repeat("x", 4096))
		for r.Context().Err() == nil {
			_, err := w.Write(chunk)
			if err != nil {
				return
			}
		}
	}))
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	errs := make(chan error, 1)
	go func() {
		errs <- writeMetricsTo(ctx, srv.URL)
	}()

	select {
	case err := <-errs:
		if err == nil {
			t.Fatal("expected an error")
		}
		if !strings.Contains(err.Error(), "500") {
			t.Fatalf("unexpected error: %.200s", err)
		}
	case <-time.After(5 * time.Second):
		cancel()
		t.Fatal("writeMetricsTo did not return: error response body is read without limit")
	}
}
