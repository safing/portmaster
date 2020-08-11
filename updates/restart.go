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
	restartPending   *abool.AtomicBool
	restartTriggered *abool.AtomicBool
)

func init() {
	restartTask = module.NewTask("automatic restart", automaticRestart)
}

func triggerRestart(delay time.Duration) {
	restartPending.Set()
	restartTask.Schedule(time.Now().Add(delay))
}

// TriggerRestartIfPending triggers an automatic restart, if one is pending. This can be used to prepone a scheduled restart if the conditions are preferable.
func TriggerRestartIfPending() {
	if restartPending.IsSet() {
		_ = automaticRestart(module.Ctx, nil)
	}
}

func automaticRestart(_ context.Context, _ *modules.Task) error {
	if restartTriggered.SetToIf(false, true) {
		log.Info("updates: initiating automatic restart")
		modules.SetExitStatusCode(RestartExitCode)
		// Do not use a worker, as this would block itself here.
		go modules.Shutdown() //nolint:errcheck
	}

	return nil
}
