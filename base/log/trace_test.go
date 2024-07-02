package log

import (
	"context"
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
