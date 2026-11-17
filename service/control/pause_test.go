package control

import (
	"errors"
	"sync"
	"testing"

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
