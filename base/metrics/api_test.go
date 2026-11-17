package metrics

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	vm "github.com/VictoriaMetrics/metrics"
)

func TestWriteMetricsToErrorWithEndlessBody(t *testing.T) { //nolint:paralleltest // Modifies package globals.
	registerPushTestMetric(t)

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

// registerPushTestMetric replaces the registry with a single test metric, so
// that there is something to push.
func registerPushTestMetric(t *testing.T) {
	t.Helper()

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
}

func TestWriteMetricsToTimeout(t *testing.T) { //nolint:paralleltest // Modifies package globals.
	registerPushTestMetric(t)

	prevTimeout := pushTimeout
	pushTimeout = 200 * time.Millisecond
	t.Cleanup(func() { pushTimeout = prevTimeout })

	// Server that accepts the request, but never answers.
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(release) }) // Runs before srv.Close.

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
		// The worker manager treats these as a clean stop and would not
		// restart the pusher.
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			t.Fatalf("timeout error must not wrap context errors, got: %v", err)
		}
	case <-time.After(5 * time.Second):
		cancel()
		t.Fatal("writeMetricsTo did not return: push request has no timeout")
	}

	// Cancellation of the worker context must still be passed through.
	cancel()
	err := writeMetricsTo(ctx, srv.URL)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got: %v", err)
	}
}
