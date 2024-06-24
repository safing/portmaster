package mgr

import (
	"sync"
	"time"
)

type taskMode int

const (
	taskModeOnDemand taskMode = iota
	taskModeDelay
	taskModeRepeat
)

// Task holds info about a task that can be scheduled for execution later.
type Task struct {
	name       string
	runChannel chan struct{}

	tickerMutex    sync.Mutex
	mode           taskMode
	runTicker      *time.Ticker
	repeatDuration time.Duration

	mgr *Manager
}

// NewTask creates a new task that can be scheduled for execution later.
// By default error/panic will be logged. For custom behavior supply errorFn, the argument is optional.
func (m *Manager) NewTask(name string, taskFn func(*WorkerCtx) error, errorFn func(c *WorkerCtx, err error, panicInfo string)) *Task {
	t := &Task{
		name:           name,
		runChannel:     make(chan struct{}),
		mgr:            m,
		mode:           taskModeOnDemand,
		repeatDuration: 0,
	}

	go t.taskLoop(taskFn, errorFn)

	return t
}

func (t *Task) initTicker(duration time.Duration) {
	t.runTicker = time.NewTicker(duration)
	go func() {
		for {
			select {
			case <-t.runTicker.C:
				t.tickerMutex.Lock()

				// Handle execution
				switch t.mode {
				case taskModeDelay:
					// Run once and disable delay
					t.Go()
					if t.repeatDuration == 0 {
						t.mode = taskModeOnDemand
						// Reset the timer with a large value so it does not eat unnecessary resources,
						t.runTicker.Reset(24 * time.Hour)
					} else {
						// Repeat was called, switch to repeat mode
						t.mode = taskModeRepeat
						t.runTicker.Reset(t.repeatDuration)
					}
				case taskModeRepeat:
					t.Go()
				case taskModeOnDemand:
					// On Demand is triggered only when the Go function as called
				}

				t.tickerMutex.Unlock()
			case <-t.mgr.Done():
				return
			}
		}
	}()
}

func (t *Task) stopTicker() {
	t.tickerMutex.Lock()
	defer t.tickerMutex.Unlock()
	if t.runTicker != nil {
		t.runTicker.Stop()
		t.runTicker = nil
	}
}

func (t *Task) taskLoop(fn func(*WorkerCtx) error, errorFn func(*WorkerCtx, error, string)) {
	t.mgr.workerStart()
	defer t.mgr.workerDone()
	defer t.stopTicker()

	w := &WorkerCtx{
		logger: t.mgr.logger.With("worker", t.name),
	}
	for {
		// Wait for a signal to run.
		select {
		case <-t.runChannel:
		case <-w.Done():
			return
		}

		panicInfo, err := t.mgr.runWorker(w, fn)
		if err != nil {
			// Handle error/panic
			if panicInfo != "" {
				t.mgr.Error(
					"worker failed",
					"err", err,
					"file", panicInfo,
				)
			} else {
				t.mgr.Error(
					"worker failed",
					"err", err,
				)
			}
			if errorFn != nil {
				errorFn(w, err, panicInfo)
			}
		}
	}
}

// Go will send request for the task to run and return immediately.
func (t *Task) Go() {
	t.runChannel <- struct{}{}
}

// Delay will schedule the task to run after the given delay.
// If there is active repeating, it will be pause until the delay has elapsed.
func (t *Task) Delay(delay time.Duration) *Task {
	t.tickerMutex.Lock()
	defer t.tickerMutex.Unlock()
	t.mode = taskModeDelay
	if t.runTicker == nil {
		t.initTicker(delay)
	} else {
		t.runTicker.Reset(delay)
	}
	return t
}

// Repeat will schedule the task to run every time duration elapses.
// If Delay was called before, the repeating will start after the first delay has elapsed.
func (t *Task) Repeat(duration time.Duration) *Task {
	t.tickerMutex.Lock()
	defer t.tickerMutex.Unlock()
	t.repeatDuration = duration

	if t.mode != taskModeDelay {
		t.mode = taskModeRepeat

		if t.runTicker == nil {
			t.initTicker(duration)
		} else {
			t.runTicker.Reset(duration)
		}
	}
	return t
}
