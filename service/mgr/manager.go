package mgr

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"
)

// ManagerNameSLogKey is used as the logging key for the name of the manager.
var ManagerNameSLogKey = "manager"

// Manager manages workers.
type Manager struct {
	name   string
	logger *slog.Logger

	ctx       context.Context
	cancelCtx context.CancelFunc

	workerCnt   atomic.Int32
	workersDone chan struct{}
}

// New returns a new manager.
func New(name string) *Manager {
	return newManager(name)
}

func newManager(name string) *Manager {
	m := &Manager{
		name:        name,
		logger:      slog.Default().With(ManagerNameSLogKey, name),
		workersDone: make(chan struct{}),
	}
	m.ctx, m.cancelCtx = context.WithCancel(context.Background())
	return m
}

// Name returns the manager name.
func (m *Manager) Name() string {
	return m.name
}

// setName sets the manager name and resets the logger to use that name.
// Not safe for concurrent use with any other module methods.
func (m *Manager) setName(newName string) {
	m.name = newName
	m.logger = slog.Default().With(ManagerNameSLogKey, m.name)
}

// Ctx returns the worker context.
func (m *Manager) Ctx() context.Context {
	return m.ctx
}

// Cancel cancels the worker context.
func (m *Manager) Cancel() {
	m.cancelCtx()
}

// Done returns the context Done channel.
func (m *Manager) Done() <-chan struct{} {
	return m.ctx.Done()
}

// IsDone checks whether the manager context is done.
func (m *Manager) IsDone() bool {
	return m.ctx.Err() != nil
}

// LogEnabled reports whether the logger emits log records at the given level.
// The manager context is automatically supplied.
func (m *Manager) LogEnabled(level slog.Level) bool {
	return m.logger.Enabled(m.ctx, level)
}

// Debug logs at LevelDebug.
// The manager context is automatically supplied.
func (m *Manager) Debug(msg string, args ...any) {
	m.logger.DebugContext(m.ctx, msg, args...)
}

// Info logs at LevelInfo.
// The manager context is automatically supplied.
func (m *Manager) Info(msg string, args ...any) {
	m.logger.InfoContext(m.ctx, msg, args...)
}

// Warn logs at LevelWarn.
// The manager context is automatically supplied.
func (m *Manager) Warn(msg string, args ...any) {
	m.logger.WarnContext(m.ctx, msg, args...)
}

// Error logs at LevelError.
// The manager context is automatically supplied.
func (m *Manager) Error(msg string, args ...any) {
	m.logger.ErrorContext(m.ctx, msg, args...)
}

// Log emits a log record with the current time and the given level and message.
// The manager context is automatically supplied.
func (m *Manager) Log(level slog.Level, msg string, args ...any) {
	m.logger.Log(m.ctx, level, msg, args...)
}

// LogAttrs is a more efficient version of Log() that accepts only Attrs.
// The manager context is automatically supplied.
func (m *Manager) LogAttrs(level slog.Level, msg string, attrs ...slog.Attr) {
	m.logger.LogAttrs(m.ctx, level, msg, attrs...)
}

// WaitForWorkers waits for all workers of this manager to be done.
// The default maximum waiting time is one minute.
func (m *Manager) WaitForWorkers(max time.Duration) (done bool) {
	// Return immediately if there are no workers.
	if m.workerCnt.Load() == 0 {
		return true
	}

	// Setup timers.
	reCheckDuration := 100 * time.Millisecond
	if max <= 0 {
		max = time.Minute
	}
	reCheck := time.NewTimer(reCheckDuration)
	maxWait := time.NewTimer(max)
	defer reCheck.Stop()
	defer maxWait.Stop()

	// Wait for workers to finish, plus check the count in intervals.
	for {
		if m.workerCnt.Load() == 0 {
			return true
		}

		select {
		case <-m.workersDone:
			return true

		case <-reCheck.C:
			// Check worker count again.
			// This is a dead simple and effective way to avoid all the channel race conditions.
			reCheckDuration *= 2
			reCheck.Reset(reCheckDuration)

		case <-maxWait.C:
			return m.workerCnt.Load() == 0
		}
	}
}

func (m *Manager) workerStart() {
	m.workerCnt.Add(1)
}

func (m *Manager) workerDone() {
	if m.workerCnt.Add(-1) == 0 {
		// Notify all waiters.
		for {
			select {
			case m.workersDone <- struct{}{}:
			default:
				return
			}
		}
	}
}

// Reset resets the manager in order to be able to be used again.
// In the process, the current context is canceled.
// As part of a module (in a group), the module might be stopped and started again.
// This method is not goroutine-safe. The caller must make sure the manager is
// not being used in any way during execution.
func (m *Manager) Reset() {
	m.cancelCtx()
	m.ctx, m.cancelCtx = context.WithCancel(context.Background())
	m.workerCnt.Store(0)
	m.workersDone = make(chan struct{})
}
