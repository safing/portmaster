package mgr

import (
	"context"
	"errors"
	"sync/atomic"
	"time"
)

// Scheduler schedules a worker.
type Scheduler struct {
	mgr *Manager
	ctx *WorkerCtx

	name string
	fn   func(w *WorkerCtx) error

	run  chan struct{}
	eval chan struct{}

	delay     atomic.Int64
	repeat    atomic.Int64
	keepAlive atomic.Bool

	errorFn func(c *WorkerCtx, err error, panicInfo string)
}

// NewScheduler creates a new scheduler for the given worker function.
// Errors and panic will only be logged by default.
// If custom behavior is required, supply an errorFn.
// If all scheduling has ended, the scheduler will end itself,
// including all related workers, except if keep-alive is enabled.
func (m *Manager) NewScheduler(name string, fn func(w *WorkerCtx) error, errorFn func(c *WorkerCtx, err error, panicInfo string)) *Scheduler {
	// Create task context.
	wCtx := &WorkerCtx{
		logger: m.logger.With("worker", name),
	}
	wCtx.ctx, wCtx.cancelCtx = context.WithCancel(m.Ctx())

	s := &Scheduler{
		mgr:     m,
		ctx:     wCtx,
		name:    name,
		fn:      fn,
		run:     make(chan struct{}, 1),
		eval:    make(chan struct{}, 1),
		errorFn: errorFn,
	}

	go s.taskMgr()
	return s
}

func (s *Scheduler) taskMgr() {
	// If the task manager ends, end all descendants too.
	defer s.ctx.cancelCtx()

	// Timers.
	var nextExecute <-chan time.Time
manage:
	for {
		// Select timer / ticker.
		switch {
		case s.delay.Swap(0) > 0:
			nextExecute = time.After(time.Duration(s.delay.Load()))

		case s.repeat.Load() > 0:
			nextExecute = time.After(time.Duration(s.repeat.Load()))

		case !s.keepAlive.Load():
			// If no delay or repeat is set, end task.
			// Except, if explicitly set to be kept alive.
			return

		default:
			// No trigger is set, disable timed execution.
			nextExecute = nil
		}

		// Wait for action or ticker.
		select {
		case <-s.run:
		case <-nextExecute:
		case <-s.eval:
			continue manage
		case <-s.ctx.Done():
			return
		}

		// Run worker.
		panicInfo, err := s.mgr.runWorker(s.ctx, s.fn)
		switch {
		case err == nil:
			// Continue with scheduling.
		case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
			// Worker was canceled, continue with scheduling.
			// A canceled context or exceeded deadline also means that the worker is finished.

		default:
			// Log error and return.
			if panicInfo != "" {
				s.ctx.Error(
					"worker failed",
					"err", err,
					"file", panicInfo,
				)
			} else {
				s.ctx.Error(
					"worker failed",
					"err", err,
				)
			}

			// Execute error function, else, end the scheduler.
			if s.errorFn != nil {
				s.errorFn(s.ctx, err, panicInfo)
			} else {
				return
			}
		}
	}
}

// Go executes the worker immediately.
// If the worker is currently being executed,
// the next execution will commence afterwards.
func (s *Scheduler) Go() {
	select {
	case s.run <- struct{}{}:
	default:
	}
}

// KeepAlive instructs the scheduler to not self-destruct,
// even if all scheduled work is complete.
func (s *Scheduler) KeepAlive() {
	s.keepAlive.Store(true)
}

// Stop immediately stops the scheduler and all related workers.
func (s *Scheduler) Stop() {
	s.ctx.cancelCtx()
}

// Delay will schedule the worker to run after the given duration.
// If set, the repeat schedule will continue afterwards.
// Disable the delay by passing 0.
func (s *Scheduler) Delay(duration time.Duration) {
	s.delay.Store(int64(duration))
	s.check()
}

// Repeat will repeatedly execute the worker using the given interval.
// Disable the repeating by passing 0.
func (s *Scheduler) Repeat(interval time.Duration) {
	s.repeat.Store(int64(interval))
	s.check()
}

func (s *Scheduler) check() {
	select {
	case s.eval <- struct{}{}:
	default:
	}
}
