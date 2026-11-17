package log

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestContextTracer(t *testing.T) {
	t.Parallel()

	// skip
	if testing.Short() {
		t.Skip()
	}

	ctx, tracer := AddTracer(context.Background())
	_ = Tracer(ctx)

	tracer.Trace("api: request received, checking security")
	time.Sleep(1 * time.Millisecond)
	tracer.Trace("login: logging in user")
	time.Sleep(1 * time.Millisecond)
	tracer.Trace("database: fetching requested resources")
	time.Sleep(10 * time.Millisecond)
	tracer.Warning("database: partial failure")
	time.Sleep(10 * time.Microsecond)
	tracer.Trace("renderer: rendering output")
	time.Sleep(1 * time.Millisecond)
	tracer.Trace("api: returning request")

	tracer.Trace("api: completed request")
	tracer.Submit()
	time.Sleep(100 * time.Millisecond)
}

// TestContextTracerSubmitDetachesLines checks that Submit hands the collected
// lines over and leaves the tracer empty, so a second Submit does not emit
// the remaining lines again.
func TestContextTracerSubmitDetachesLines(t *testing.T) {
	t.Parallel()

	tracer := &ContextTracer{}
	tracer.Trace("first")
	tracer.Trace("second")
	tracer.Trace("third")
	tracer.Submit()

	tracer.Lock()
	remaining := len(tracer.logs)
	tracer.Unlock()
	if remaining != 0 {
		t.Fatalf("expected tracer to be empty after Submit, still holds %d lines", remaining)
	}
}

// TestContextTracerConcurrentSubmit uses one tracer from multiple goroutines,
// tracing and submitting concurrently, while the log writer formats the
// submitted lines. Run with -race.
func TestContextTracerConcurrentSubmit(t *testing.T) {
	t.Parallel()

	tracer := &ContextTracer{}

	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			for range 200 {
				tracer.Trace("step")
				tracer.Warning("something")
				tracer.Submit()
			}
		})
	}
	wg.Wait()

	// Give the writer time to process the submitted lines.
	time.Sleep(50 * time.Millisecond)
}
