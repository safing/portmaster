package mgr

import (
	"context"
	"errors"
	"sync"
	"time"
)

// WorkerMgr schedules a worker.
type WorkerMgr struct {
	mgr *Manager
	ctx *WorkerCtx

	// Definition.
	name    string
	fn      func(w *WorkerCtx) error
	errorFn func(c *WorkerCtx, err error, panicInfo string)

	// Manual trigger.
	run chan struct{}

	// Actions.
	actionLock   sync.Mutex
	selectAction chan struct{}
	delay        *workerMgrDelay
	repeat       *workerMgrRepeat
	keepAlive    *workerMgrNoop
}

type taskAction interface {
	Wait() <-chan time.Time
	Ack()
}

// Delay.
type workerMgrDelay struct {
	s     *WorkerMgr
	timer *time.Timer
}

func (s *WorkerMgr) newDelay(duration time.Duration) *workerMgrDelay {
	return &workerMgrDelay{
		s:     s,
		timer: time.NewTimer(duration),
	}
}
func (sd *workerMgrDelay) Wait() <-chan time.Time { return sd.timer.C }

func (sd *workerMgrDelay) Ack() {
	sd.s.actionLock.Lock()
	defer sd.s.actionLock.Unlock()

	// Remove delay, as it can only fire once.
	sd.s.delay = nil

	// Reset repeat.
	sd.s.repeat.Reset()

	// Stop timer.
	sd.timer.Stop()
}

func (sd *workerMgrDelay) Stop() {
	if sd == nil {
		return
	}
	sd.timer.Stop()
}

// Repeat.
type workerMgrRepeat struct {
	ticker   *time.Ticker
	interval time.Duration
}

func (s *WorkerMgr) newRepeat(interval time.Duration) *workerMgrRepeat {
	return &workerMgrRepeat{
		ticker:   time.NewTicker(interval),
		interval: interval,
	}
}

func (sr *workerMgrRepeat) Wait() <-chan time.Time { return sr.ticker.C }
func (sr *workerMgrRepeat) Ack()                   {}

func (sr *workerMgrRepeat) Reset() {
	if sr == nil {
		return
	}
	sr.ticker.Reset(sr.interval)
}

func (sr *workerMgrRepeat) Stop() {
	if sr == nil {
		return
	}
	sr.ticker.Stop()
}

// Noop.
type workerMgrNoop struct{}

func (sn *workerMgrNoop) Wait() <-chan time.Time { return nil }
func (sn *workerMgrNoop) Ack()                   {}

// NewWorkerMgr creates a new scheduler for the given worker function.
// Errors and panic will only be logged by default.
// If custom behavior is required, supply an errorFn.
// If all scheduling has ended, the scheduler will end itself,
// including all related workers, except if keep-alive is enabled.
func (m *Manager) NewWorkerMgr(name string, fn func(w *WorkerCtx) error, errorFn func(c *WorkerCtx, err error, panicInfo string)) *WorkerMgr {
	// Create task context.
	wCtx := &WorkerCtx{
		logger: m.logger.With("worker", name),
	}
	wCtx.ctx, wCtx.cancelCtx = context.WithCancel(m.Ctx())

	s := &WorkerMgr{
		mgr:          m,
		ctx:          wCtx,
		name:         name,
		fn:           fn,
		errorFn:      errorFn,
		run:          make(chan struct{}, 1),
		selectAction: make(chan struct{}, 1),
	}
	wCtx.workerMgr = s

	go s.taskMgr()
	return s
}

func (s *WorkerMgr) taskMgr() {
	s.mgr.workerStart(s.ctx)
	defer s.mgr.workerDone(s.ctx)

	// If the task manager ends, end all descendants too.
	defer s.ctx.cancelCtx()

	// Timers and tickers.
	var (
		action taskAction
	)
	defer func() {
		s.delay.Stop()
		s.repeat.Stop()
	}()

	// Wait for the first action.
	select {
	case <-s.selectAction:
	case <-s.ctx.Done():
		return
	}

manage:
	for {
		// Select action.
		func() {
			s.actionLock.Lock()
			defer s.actionLock.Unlock()

			switch {
			case s.delay != nil:
				action = s.delay
			case s.repeat != nil:
				action = s.repeat
			case s.keepAlive != nil:
				action = s.keepAlive
			default:
				action = nil
			}
		}()
		if action == nil {
			return
		}

		// Wait for trigger or action.
		select {
		case <-action.Wait():
			action.Ack()
			// Time-triggered execution.
		case <-s.run:
			// Manually triggered execution.
		case <-s.selectAction:
			// Re-select action.
			continue manage
		case <-s.ctx.Done():
			// Abort!
			return
		}

		// Run worker.
		wCtx := &WorkerCtx{
			workerMgr: s,
			logger:    s.ctx.logger,
		}
		//nolint:fatcontext // Every run gets a new context.
		wCtx.ctx, wCtx.cancelCtx = context.WithCancel(s.ctx.ctx)
		panicInfo, err := s.mgr.runWorker(wCtx, s.fn)

		switch {
		case err == nil:
			// Continue with scheduling.
		case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
			// Worker was canceled, continue with scheduling.
			// A canceled context or exceeded deadline also means that the worker is finished.

		default:
			// Log error and return.
			if panicInfo != "" {
				wCtx.Error(
					"worker failed",
					"err", err,
					"file", panicInfo,
				)
			} else {
				wCtx.Error(
					"worker failed",
					"err", err,
				)
			}

			// Delegate error handling to the error function, otherwise just continue the scheduler.
			// The error handler can stop the scheduler if it wants to.
			if s.errorFn != nil {
				s.errorFn(s.ctx, err, panicInfo)
			}
		}
	}
}

// Status returns the current status of the worker manager.
func (s *WorkerMgr) Status() string {
	s.actionLock.Lock()
	defer s.actionLock.Unlock()

	switch {
	case s.delay != nil:
		return "delayed"
	case s.repeat != nil:
		return "repeated every " + s.repeat.interval.String()
	case s.keepAlive != nil:
		return "on demand"
	default:
		return "created"
	}
}

// Go executes the worker immediately.
// If the worker is currently being executed,
// the next execution will commence afterwards.
// Calling Go() implies KeepAlive() if nothing else was specified yet.
func (s *WorkerMgr) Go() {
	s.actionLock.Lock()
	defer s.actionLock.Unlock()

	// Check if any action is already defined.
	switch {
	case s.delay != nil:
	case s.repeat != nil:
	case s.keepAlive != nil:
	default:
		s.keepAlive = &workerMgrNoop{}
		s.check()
	}

	// Reset repeat if set.
	s.repeat.Reset()

	// Stop delay if set.
	s.delay.Stop()
	s.delay = nil

	// Send run command
	select {
	case s.run <- struct{}{}:
	default:
	}
}

// Stop immediately stops the scheduler and all related workers.
func (s *WorkerMgr) Stop() {
	s.ctx.cancelCtx()
}

// Delay will schedule the worker to run after the given duration.
// If set, the repeat schedule will continue afterwards.
// Disable the delay by passing 0.
func (s *WorkerMgr) Delay(duration time.Duration) *WorkerMgr {
	s.actionLock.Lock()
	defer s.actionLock.Unlock()

	s.delay.Stop()
	s.delay = nil
	if duration > 0 {
		s.delay = s.newDelay(duration)
	}

	s.check()
	return s
}

// Repeat will repeatedly execute the worker using the given interval.
// Disable repeating by passing 0.
func (s *WorkerMgr) Repeat(interval time.Duration) *WorkerMgr {
	s.actionLock.Lock()
	defer s.actionLock.Unlock()

	s.repeat.Stop()
	s.repeat = nil
	if interval > 0 {
		s.repeat = s.newRepeat(interval)
	}

	s.check()
	return s
}

// KeepAlive instructs the scheduler to not self-destruct,
// even if all scheduled work is complete.
func (s *WorkerMgr) KeepAlive() *WorkerMgr {
	s.actionLock.Lock()
	defer s.actionLock.Unlock()

	s.keepAlive = &workerMgrNoop{}

	s.check()
	return s
}

func (s *WorkerMgr) check() {
	select {
	case s.selectAction <- struct{}{}:
	default:
	}
}
