package mgr

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"runtime"
	"runtime/debug"
	"strings"
	"time"
)

// workerContextKey is a key used for the context key/value storage.
type workerContextKey struct{}

// WorkerCtxContextKey is the key used to add the WorkerCtx to a context.
var WorkerCtxContextKey = workerContextKey{}

// WorkerCtx provides workers with the necessary environment for flow control
// and logging.
type WorkerCtx struct {
	name     string
	workFunc func(w *WorkerCtx) error

	ctx       context.Context
	cancelCtx context.CancelFunc

	workerMgr *WorkerMgr // TODO: Attach to context instead?
	logger    *slog.Logger
}

// AddToCtx adds the WorkerCtx to the given context.
func (w *WorkerCtx) AddToCtx(ctx context.Context) context.Context {
	return context.WithValue(ctx, WorkerCtxContextKey, w)
}

// WorkerFromCtx returns the WorkerCtx from the given context.
func WorkerFromCtx(ctx context.Context) *WorkerCtx {
	v := ctx.Value(WorkerCtxContextKey)
	if w, ok := v.(*WorkerCtx); ok {
		return w
	}
	return nil
}

// Ctx returns the worker context.
// Is automatically canceled after the worker stops/returns, regardless of error.
func (w *WorkerCtx) Ctx() context.Context {
	return w.ctx
}

// Cancel cancels the worker context.
// Is automatically called after the worker stops/returns, regardless of error.
func (w *WorkerCtx) Cancel() {
	w.cancelCtx()
}

// WorkerMgr returns the worker manager the worker was started from.
// Returns nil if the worker is not associated with a scheduler.
func (w *WorkerCtx) WorkerMgr() *WorkerMgr {
	return w.workerMgr
}

// Done returns the context Done channel.
func (w *WorkerCtx) Done() <-chan struct{} {
	return w.ctx.Done()
}

// IsDone checks whether the worker context is done.
func (w *WorkerCtx) IsDone() bool {
	return w.ctx.Err() != nil
}

// Logger returns the logger used by the worker context.
func (w *WorkerCtx) Logger() *slog.Logger {
	return w.logger
}

// LogEnabled reports whether the logger emits log records at the given level.
// The worker context is automatically supplied.
func (w *WorkerCtx) LogEnabled(level slog.Level) bool {
	return w.logger.Enabled(w.ctx, level)
}

// Debug logs at LevelDebug.
// The worker context is automatically supplied.
func (w *WorkerCtx) Debug(msg string, args ...any) {
	if !w.logger.Enabled(w.ctx, slog.LevelDebug) {
		return
	}
	w.writeLog(slog.LevelDebug, msg, args...)
}

// Info logs at LevelInfo.
// The worker context is automatically supplied.
func (w *WorkerCtx) Info(msg string, args ...any) {
	if !w.logger.Enabled(w.ctx, slog.LevelInfo) {
		return
	}
	w.writeLog(slog.LevelInfo, msg, args...)
}

// Warn logs at LevelWarn.
// The worker context is automatically supplied.
func (w *WorkerCtx) Warn(msg string, args ...any) {
	if !w.logger.Enabled(w.ctx, slog.LevelWarn) {
		return
	}
	w.writeLog(slog.LevelWarn, msg, args...)
}

// Error logs at LevelError.
// The worker context is automatically supplied.
func (w *WorkerCtx) Error(msg string, args ...any) {
	if !w.logger.Enabled(w.ctx, slog.LevelError) {
		return
	}
	w.writeLog(slog.LevelError, msg, args...)
}

// Log emits a log record with the current time and the given level and message.
// The worker context is automatically supplied.
func (w *WorkerCtx) Log(level slog.Level, msg string, args ...any) {
	if !w.logger.Enabled(w.ctx, level) {
		return
	}
	w.writeLog(level, msg, args...)
}

// LogAttrs is a more efficient version of Log() that accepts only Attrs.
// The worker context is automatically supplied.
func (w *WorkerCtx) LogAttrs(level slog.Level, msg string, attrs ...slog.Attr) {
	if !w.logger.Enabled(w.ctx, level) {
		return
	}

	var pcs [1]uintptr
	runtime.Callers(2, pcs[:]) // skip "Callers" and "LogAttrs".
	r := slog.NewRecord(time.Now(), level, msg, pcs[0])
	r.AddAttrs(attrs...)
	_ = w.logger.Handler().Handle(w.ctx, r)
}

func (w *WorkerCtx) writeLog(level slog.Level, msg string, args ...any) {
	var pcs [1]uintptr
	runtime.Callers(3, pcs[:]) // skip "Callers", "writeLog" and the calling function.
	r := slog.NewRecord(time.Now(), level, msg, pcs[0])
	r.Add(args...)
	_ = w.logger.Handler().Handle(w.ctx, r)
}

// Go starts the given function in a goroutine (as a "worker").
// The worker context has
// - A separate context which is canceled when the functions returns.
// - Access to named structure logging.
// - Given function is re-run after failure (with backoff).
// - Panic catching.
// - Flow control helpers.
func (m *Manager) Go(name string, fn func(w *WorkerCtx) error) {
	// m.logger.Log(m.ctx, slog.LevelInfo, "worker started", "name", name)
	go m.manageWorker(name, fn)
}

func (m *Manager) manageWorker(name string, fn func(w *WorkerCtx) error) {
	w := &WorkerCtx{
		name:     name,
		workFunc: fn,
		logger:   m.logger.With("worker", name),
	}
	w.ctx = m.ctx

	m.workerStart(w)
	defer m.workerDone(w)

	backoff := time.Second
	failCnt := 0

	for {
		panicInfo, err := m.runWorker(w, fn)
		switch {
		case err == nil:
			// No error means that the worker is finished.
			return

		case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
			// A canceled context or exceeded deadline also means that the worker is finished.
			return

		default:
			// Any other errors triggers a restart with backoff.

			// If manager is stopping, just log error and return.
			if m.IsDone() {
				if panicInfo != "" {
					w.Error(
						"worker failed",
						"err", err,
						"file", panicInfo,
					)
				} else {
					w.Error(
						"worker failed",
						"err", err,
					)
				}
				return
			}

			// Count failure and increase backoff (up to limit),
			failCnt++
			backoff *= 2
			if backoff > time.Minute {
				backoff = time.Minute
			}

			// Log error and retry after backoff duration.
			if panicInfo != "" {
				w.Error(
					"worker failed",
					"failCnt", failCnt,
					"backoff", backoff,
					"err", err,
					"file", panicInfo,
				)
			} else {
				w.Error(
					"worker failed",
					"failCnt", failCnt,
					"backoff", backoff,
					"err", err,
				)
			}
			select {
			case <-time.After(backoff):
			case <-m.ctx.Done():
				return
			}
		}
	}
}

// Do directly executes the given function (as a "worker").
// The worker context has
// - A separate context which is canceled when the functions returns.
// - Access to named structure logging.
// - Given function is re-run after failure (with backoff).
// - Panic catching.
// - Flow control helpers.
func (m *Manager) Do(name string, fn func(w *WorkerCtx) error) error {
	// Create context.
	w := &WorkerCtx{
		name:     name,
		workFunc: fn,
		ctx:      m.Ctx(),
		logger:   m.logger.With("worker", name),
	}

	m.workerStart(w)
	defer m.workerDone(w)

	// Run worker.
	panicInfo, err := m.runWorker(w, fn)
	switch {
	case err == nil:
		// No error means that the worker is finished.
		return nil

	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		// A canceled context or exceeded deadline also means that the worker is finished.
		return err

	default:
		// Log error and return.
		if panicInfo != "" {
			w.Error(
				"worker failed",
				"err", err,
				"file", panicInfo,
			)
		} else {
			w.Error(
				"worker failed",
				"err", err,
			)
		}
		return err
	}
}

func (m *Manager) runWorker(w *WorkerCtx, fn func(w *WorkerCtx) error) (panicInfo string, err error) {
	// Create worker context that is canceled when worker finished or dies.
	w.ctx, w.cancelCtx = context.WithCancel(w.ctx)
	defer w.Cancel()

	// Recover from panic.
	defer func() {
		panicVal := recover()
		if panicVal != nil {
			err = fmt.Errorf("panic: %s", panicVal)

			// Print panic to stderr.
			stackTrace := string(debug.Stack())
			fmt.Fprintf(
				os.Stderr,
				"===== PANIC =====\n%s\n\n%s=====  END  =====\n",
				panicVal,
				stackTrace,
			)

			// Find the line in the stack trace that refers to where the panic occurred.
			stackLines := strings.Split(stackTrace, "\n")
			foundPanic := false
			for i, line := range stackLines {
				if !foundPanic {
					if strings.Contains(line, "panic(") {
						foundPanic = true
					}
				} else {
					if strings.Contains(line, "portmaster") {
						if i+1 < len(stackLines) {
							panicInfo = strings.SplitN(strings.TrimSpace(stackLines[i+1]), " ", 2)[0]
						}
						break
					}
				}
			}
		}
	}()

	err = fn(w)
	return //nolint
}

// Repeat executes the given function periodically in a goroutine (as a "worker").
// The worker context has
// - A separate context which is canceled when the functions returns.
// - Access to named structure logging.
// - By default error/panic will be logged. For custom behavior supply errorFn, the argument is optional.
// - Flow control helpers.
// - Repeat is intended for long running tasks that are mostly idle.
func (m *Manager) Repeat(name string, period time.Duration, fn func(w *WorkerCtx) error) *WorkerMgr {
	t := m.NewWorkerMgr(name, fn, nil)
	return t.Repeat(period)
}

// Delay starts the given function delayed in a goroutine (as a "worker").
// The worker context has
// - A separate context which is canceled when the functions returns.
// - Access to named structure logging.
// - By default error/panic will be logged. For custom behavior supply errorFn, the argument is optional.
// - Panic catching.
// - Flow control helpers.
func (m *Manager) Delay(name string, period time.Duration, fn func(w *WorkerCtx) error) *WorkerMgr {
	t := m.NewWorkerMgr(name, fn, nil)
	return t.Delay(period)
}
