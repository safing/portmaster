package updates

import (
	"context"
	"time"

	"github.com/safing/portbase/log"
	"github.com/safing/portbase/modules"
	"github.com/tevino/abool"
)

const (
	// RestartExitCode will instruct portmaster-start to restart the process immediately, potentially with a new version.
	RestartExitCode = 23
)

var (
	restartTask      *modules.Task
	restartPending   = abool.New()
	restartTriggered = abool.New()
)

// DelayedRestart triggers a restart of the application by shutting down the
// module system gracefully and returning with RestartExitCode. The restart
// may be further delayed by up to 10 minutes by the internal task scheduling
// system. This only works if the process is managed by portmaster-start.
func DelayedRestart(delay time.Duration) {
	log.Warningf("updates: restart triggered, will execute in %s", delay)

	// This enables TriggerRestartIfPending.
	// Subsequent calls to TriggerRestart should be able to set a new delay.
	restartPending.Set()

	// Schedule the restart task.
	restartTask.Schedule(time.Now().Add(delay))
}

// TriggerRestartIfPending triggers an automatic restart, if one is pending.
// This can be used to prepone a scheduled restart if the conditions are preferable.
func TriggerRestartIfPending() {
	if restartPending.IsSet() {
		_ = automaticRestart(module.Ctx, nil)
	}
}

// RestartNow immediately executes a restart.
// This only works if the process is managed by portmaster-start.
func RestartNow() {
	_ = automaticRestart(module.Ctx, nil)
}

func automaticRestart(_ context.Context, _ *modules.Task) error {
	if restartTriggered.SetToIf(false, true) {
		log.Info("updates: initiating (automatic) restart")
		modules.SetExitStatusCode(RestartExitCode)
		// Do not use a worker, as this would block itself here.
		go modules.Shutdown() //nolint:errcheck
	}

	return nil
}
