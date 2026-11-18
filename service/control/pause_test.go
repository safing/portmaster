package control

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/safing/portmaster/base/api"
	"github.com/safing/portmaster/base/notifications"
	"github.com/safing/portmaster/service/mgr"
)

const (
	pausedEventID = "control:paused"
	errorEventID  = "control:error"
)

var (
	errTestResume          = errors.New("test error")
	notificationsSetupOnce sync.Once
)

func newTestControl(t *testing.T) *Control {
	t.Helper()

	notificationsSetupOnce.Do(func() {
		_, err := notifications.New(struct{}{})
		if err != nil {
			t.Fatalf("failed to create notifications module: %s", err)
		}
	})

	m := mgr.New("Control")
	return &Control{
		mgr:    m,
		states: mgr.NewStateMgr(m),
	}
}

func assertNoPauseLeftovers(t *testing.T, c *Control) {
	t.Helper()

	for _, id := range []string{pausedEventID, errorEventID} {
		if n := notifications.Get(id); n != nil {
			t.Errorf("notification %q still shown after successful resume", id)
		}
	}
	if states := c.states.Export().States; len(states) != 0 {
		t.Errorf("module states left after successful resume: %+v", states)
	}
}

// TestPauseNotificationsClearedAfterFailedResume checks that a failed resume
// (error notification) followed by a successful resume does not leave the
// "paused" or the error notification, nor their module states, behind.
//
//nolint:paralleltest // Uses the global notification storage.
func TestPauseNotificationsClearedAfterFailedResume(t *testing.T) {
	c := newTestControl(t)

	// Paused.
	c.pauseInfo.Interception = true
	c.updateStatesAndNotify()
	if notifications.Get(pausedEventID) == nil {
		t.Fatal("pause notification was not shown")
	}

	// Resume fails: still paused, error is shown.
	c.updateStatesAndNotifyError("Resume operation failed", errTestResume)
	if notifications.Get(errorEventID) == nil {
		t.Fatal("error notification was not shown")
	}

	// Resume succeeds.
	c.pauseInfo.Interception = false
	c.updateStatesAndNotify()

	assertNoPauseLeftovers(t, c)
}

// TestErrorNotificationClearedByNextPause checks that the error notification
// of a failed resume is removed by the next pause/resume attempt instead of
// being orphaned when the pause notification is shown again.
//
//nolint:paralleltest // Uses the global notification storage.
func TestErrorNotificationClearedByNextPause(t *testing.T) {
	c := newTestControl(t)

	c.pauseInfo.Interception = true
	c.updateStatesAndNotify()
	c.updateStatesAndNotifyError("Resume operation failed", errTestResume)

	// Pause again (e.g. with a new duration), then resume successfully.
	c.updateStatesAndNotify()
	c.pauseInfo.Interception = false
	c.updateStatesAndNotify()

	assertNoPauseLeftovers(t, c)
}

type controlTestInstance struct {
	interception *mgr.GroupModule
}

func (i *controlTestInstance) InterceptionGroup() *mgr.GroupModule { return i.interception }
func (i *controlTestInstance) IsShuttingDown() bool                { return false }

func newTestControlWithInstance(t *testing.T) *Control {
	t.Helper()

	c := newTestControl(t)
	c.instance = &controlTestInstance{
		interception: mgr.NewGroupModule("Interception"),
	}
	return c
}

// TestSupersededResumeWorkerDoesNotResume checks that a resume worker whose
// timer fired while another pause request held the lock and replaced it does
// not resume afterwards, undoing the new pause.
//
//nolint:paralleltest // Uses the global notification storage.
func TestSupersededResumeWorkerDoesNotResume(t *testing.T) {
	c := newTestControlWithInstance(t)
	defer c.mgr.Cancel()

	// Pause for a short time.
	err := c.pause(20 * time.Millisecond)
	if err != nil {
		t.Fatalf("pause failed: %s", err)
	}

	// Simulate a second pause request that holds the lock while the first
	// resume timer fires, and then replaces the resume worker.
	c.locker.Lock()
	time.Sleep(100 * time.Millisecond) // Let the old resume worker block on the lock.
	c.stopResumeWorker()
	c.startResumeWorker(time.Hour)
	c.locker.Unlock()

	// Give the old resume worker the chance to (wrongly) resume.
	time.Sleep(50 * time.Millisecond)

	c.locker.Lock()
	paused := c.pauseInfo.Interception
	c.stopResumeWorker()
	c.pauseInfo.Interception = false
	c.updateStatesAndNotify()
	c.locker.Unlock()

	if !paused {
		t.Fatal("superseded resume worker resumed although a new pause was requested")
	}
}

// TestResumeWorkerResumesAfterDuration checks that a pause still ends
// automatically after its duration.
//
//nolint:paralleltest // Uses the global notification storage.
func TestResumeWorkerResumesAfterDuration(t *testing.T) {
	c := newTestControlWithInstance(t)
	defer c.mgr.Cancel()

	err := c.pause(20 * time.Millisecond)
	if err != nil {
		t.Fatalf("pause failed: %s", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		c.locker.Lock()
		paused := c.pauseInfo.Interception
		c.locker.Unlock()
		if !paused {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("pause did not end after its duration")
}

// TestPauseOnlySPNRejected checks that a request to pause only the SPN, as
// sent by older clients, is rejected and does not pause the interception.
//
//nolint:paralleltest // Uses the global notification storage.
func TestPauseOnlySPNRejected(t *testing.T) {
	c := newTestControlWithInstance(t)
	defer c.mgr.Cancel()

	_, err := c.handlePause(&api.Request{InputData: []byte(`{"duration":60,"onlySPN":true}`)})
	if !errors.Is(err, errOnlySPNNotSupported) {
		t.Fatalf("expected %v, got %v", errOnlySPNNotSupported, err)
	}
	if c.pauseInfo.Interception || c.pauseInfo.SPN {
		t.Fatalf("pause state changed by rejected request: %+v", c.pauseInfo)
	}
}
