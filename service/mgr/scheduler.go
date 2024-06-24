package mgr

import (
	"context"
	"errors"
	"sync"
	"time"
)

// Scheduler schedules a worker.
type Scheduler struct {
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
	delay        *schedulerDelay
	repeat       *schedulerRepeat
	keepAlive    *schedulerNoop
}

type schedulerAction interface {
	Wait() <-chan time.Time
	Ack()
}

type schedulerDelay struct {
	s     *Scheduler
	timer *time.Timer
}

func (s *Scheduler) newDelay(duration time.Duration) *schedulerDelay {
	return &schedulerDelay{
		s:     s,
		timer: time.NewTimer(duration),
	}
}
func (sd *schedulerDelay) Wait() <-chan time.Time { return sd.timer.C }

func (sd *schedulerDelay) Ack() {
	sd.s.actionLock.Lock()
	defer sd.s.actionLock.Unlock()

	// Remove delay, as it can only fire once.
	sd.s.delay = nil

	// Reset repeat.
	sd.s.repeat.Reset()

	// Stop timer.
	sd.timer.Stop()
}

func (sd *schedulerDelay) Stop() {
	if sd == nil {
		return
	}
	sd.timer.Stop()
}

type schedulerRepeat struct {
	ticker   *time.Ticker
	interval time.Duration
}

func (s *Scheduler) newRepeat(interval time.Duration) *schedulerRepeat {
	return &schedulerRepeat{
		ticker:   time.NewTicker(interval),
		interval: interval,
	}
}

func (sr *schedulerRepeat) Wait() <-chan time.Time { return sr.ticker.C }
func (sr *schedulerRepeat) Ack()                   {}

func (sr *schedulerRepeat) Reset() {
	if sr == nil {
		return
	}
	sr.ticker.Reset(sr.interval)
}

func (sr *schedulerRepeat) Stop() {
	if sr == nil {
		return
	}
	sr.ticker.Stop()
}

type schedulerNoop struct{}

func (sn *schedulerNoop) Wait() <-chan time.Time { return nil }
func (sn *schedulerNoop) Ack()                   {}

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
		mgr:          m,
		ctx:          wCtx,
		name:         name,
		fn:           fn,
		errorFn:      errorFn,
		run:          make(chan struct{}, 1),
		selectAction: make(chan struct{}, 1),
	}

	go s.taskMgr()
	return s
}

func (s *Scheduler) taskMgr() {
	s.mgr.workerStart()
	defer s.mgr.workerDone()

	// If the task manager ends, end all descendents too.
	defer s.ctx.cancelCtx()

	// Timers and tickers.
	var (
		action schedulerAction
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

			// Delegate error handling to the error function, otherwise just continue the scheduler.
			// The error handler can stop the scheduler if it wants to.
			if s.errorFn != nil {
				s.errorFn(s.ctx, err, panicInfo)
			}
		}
	}
}

// Go executes the worker immediately.
// If the worker is currently being executed,
// the next execution will commence afterwards.
// Can only be called after calling one of Delay(), Repeat() or KeepAlive().
func (s *Scheduler) Go() {
	select {
	case s.run <- struct{}{}:
	default:
	}
}

// Stop immediately stops the scheduler and all related workers.
func (s *Scheduler) Stop() {
	s.ctx.cancelCtx()
}

// Delay will schedule the worker to run after the given duration.
// If set, the repeat schedule will continue afterwards.
// Disable the delay by passing 0.
func (s *Scheduler) Delay(duration time.Duration) *Scheduler {
	s.actionLock.Lock()
	defer s.actionLock.Unlock()

	s.delay.Stop()
	s.delay = s.newDelay(duration)

	s.check()
	return s
}

// Repeat will repeatedly execute the worker using the given interval.
// Disable repeating by passing 0.
func (s *Scheduler) Repeat(interval time.Duration) *Scheduler {
	s.actionLock.Lock()
	defer s.actionLock.Unlock()

	s.repeat.Stop()
	s.repeat = s.newRepeat(interval)

	s.check()
	return s
}

// KeepAlive instructs the scheduler to not self-destruct,
// even if all scheduled work is complete.
func (s *Scheduler) KeepAlive() *Scheduler {
	s.actionLock.Lock()
	defer s.actionLock.Unlock()

	s.keepAlive = &schedulerNoop{}
	return s
}

func (s *Scheduler) check() {
	select {
	case s.selectAction <- struct{}{}:
	default:
	}
}
