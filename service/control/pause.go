package control

import (
	"errors"
	"fmt"
	"time"

	"github.com/safing/portmaster/base/config"
	"github.com/safing/portmaster/base/notifications"
	"github.com/safing/portmaster/service/mgr"
)

func (c *Control) pause(duration time.Duration, onlySPN bool) (retErr error) {
	if c.instance.IsShuttingDown() {
		c.mgr.Debug("Cannot pause: system is shutting down")
		return nil
	}

	c.locker.Lock()
	defer c.locker.Unlock()

	defer func() {
		// update states after pause attempt
		c.updateStatesAndNotify()
		// log error if pause failed
		if retErr != nil {
			c.mgr.Error("Failed to pause: " + retErr.Error())
		}
	}()

	if duration <= 0 {
		return errors.New("invalid pause duration")
	}

	if onlySPN {
		if c.pauseInfo.Interception {
			return errors.New("cannot pause SPN separately when core is paused")
		}
		// If SPN is not running and not already paused, cannot pause it or change pause duration.
		if !c.cfgSpnEnabled() && !c.pauseInfo.SPN {
			return errors.New("cannot pause SPN when it is not running")
		}
	}

	// Stop resume worker if running and start a new one later.
	c.stopResumeWorker()
	defer func() {
		if retErr == nil {
			// start new resume worker (with new duration) if no error
			c.startResumeWorker(duration)
		}
	}()

	// Pause SPN if not already paused.
	if !c.pauseInfo.SPN {
		if c.cfgSpnEnabled() {
			// "spn/access" module is responsible for starting/stopping SPN service.
			// Here we just change the config to notify it to stop SPN.
			// TODO: the 'pause' state must not make permanent config changes.
			//       Consider possibility to not store permanent config changes.
			//       E.g. SPN enabled -> pause SPN -> restart PC/Portmaster -> SPN should be enabled again.
			config.SetConfigOption("spn/enable", false)

			// Wait until SPN is fully stopped with timeout 30s.
			err := c.waitSPNStopped(time.Second * 30)
			if err != nil {
				config.SetConfigOption("spn/enable", true) // revert config change on error
				return err
			}

			c.mgr.Info("SPN paused")
			c.pauseInfo.SPN = true
		}
	}

	if onlySPN {
		return nil
	}

	if !c.pauseInfo.Interception {
		if err := c.instance.InterceptionGroup().Stop(); err != nil {
			return err
		}
		c.mgr.Info("interception paused")
		c.pauseInfo.Interception = true
	}

	return nil
}

func (c *Control) resume() (retErr error) {
	if c.instance.IsShuttingDown() {
		c.mgr.Debug("Cannot resume: system is shutting down")
		return nil
	}

	c.locker.Lock()
	defer c.locker.Unlock()

	defer func() {
		if retErr != nil {
			c.updateStatesAndNotifyError("Resume operation failed", retErr)
			c.mgr.Error("Error occurred while resuming: " + retErr.Error())
		} else {
			c.updateStatesAndNotify()
		}
	}()

	c.stopResumeWorker()

	if c.pauseInfo.Interception {
		if err := c.instance.InterceptionGroup().Start(); err != nil {
			return err
		}
		c.mgr.Info("interception resumed")
		c.pauseInfo.Interception = false
	}

	if c.pauseInfo.SPN {
		// "spn/access" module is responsible for starting/stopping SPN service.
		// Here we just change the config to notify it to start SPN.
		if !c.cfgSpnEnabled() {
			config.SetConfigOption("spn/enable", true)
			c.mgr.Info("SPN resumed")
		}
		c.pauseInfo.SPN = false
	}

	return nil
}

// stopResumeWorker stops any existing resume worker.
// No thread safety, caller must hold c.locker.
func (c *Control) stopResumeWorker() {
	c.pauseInfo.TillTime = time.Time{}

	if c.resumeWorker != nil {
		c.resumeWorker.Stop()
		c.resumeWorker = nil
	}
}

// startResumeWorker starts a worker that will resume normal operation after the specified duration.
// No thread safety, caller must hold c.locker.
func (c *Control) startResumeWorker(duration time.Duration) {
	// Strip monotonic clock from deadline to force wall-clock comparison.
	// This is critical for VM suspend/resume and system sleep scenarios.
	deadline := time.Now().Round(0).Add(duration)
	c.pauseInfo.TillTime = deadline

	resumerWorkerFunc := func(wc *mgr.WorkerCtx) error {
		wc.Info(fmt.Sprintf("Scheduling resume in %v", duration))

		// Subscribe to config changes to detect SPN enable.
		cfgChangeEvt := c.instance.Config().EventConfigChange.Subscribe("control: spn enable check", 10)
		defer cfgChangeEvt.Cancel()

		// Timer for the deadline.
		timer := time.NewTimer(time.Until(deadline))
		defer timer.Stop()
		// Periodically check resume time to handle unexpected wall-clock changes.
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()

		// Wait until duration elapses or SPN is enabled by user.
		needToAutoResume := false
		for !needToAutoResume {
			select {
			case <-wc.Ctx().Done():
				return nil
			case <-cfgChangeEvt.Events():
				if c.cfgSpnEnabled() {
					cfgChangeEvt.Cancel() // we do not need it anymore (no problem to cancel multiple times)
					wc.Info("SPN enabled by user. Auto-resume initiated.")
					needToAutoResume = true
				}
			case <-ticker.C:
				// Check wall-clock time.
				if time.Now().Round(0).After(deadline) {
					needToAutoResume = true
				}
			case <-timer.C:
				needToAutoResume = true
			}
		}

		// Time to resume
		wc.Info("Resuming...")
		err := c.resume()
		if err == nil {
			n := &notifications.Notification{
				EventID:      "control:resumed",
				Type:         notifications.Info,
				Title:        "Resumed",
				Message:      "Automatically resumed from pause state",
				ShowOnSystem: true,
				Expires:      time.Now().Add(15 * time.Second).Unix(),
				AvailableActions: []*notifications.Action{
					{
						ID:   "ack",
						Text: "OK",
					},
				},
			}
			notifications.Notify(n)
		}
		return err
	}

	c.resumeWorker = c.mgr.NewWorkerMgr("resumer", resumerWorkerFunc, nil)
	c.resumeWorker.Go()
}

// updateStatesAndNotify updates the paused states and sends notifications accordingly.
// No thread safety, caller must hold c.locker.
func (c *Control) updateStatesAndNotify() {
	if !c.pauseInfo.Interception && !c.pauseInfo.SPN {
		if c.pauseNotification != nil {
			c.pauseNotification.Delete()
			c.pauseNotification = nil
		}
		return
	}

	title := ""
	nType := notifications.Warning
	if c.pauseInfo.Interception && c.pauseInfo.SPN {
		title = "Portmaster and SPN paused"
	} else if c.pauseInfo.Interception {
		title = "Portmaster paused"
	} else if c.pauseInfo.SPN {
		title = "SPN paused"
		nType = notifications.Info // less severe notification for SPN-only pause
	}
	message := fmt.Sprintf("%s until %v", title, c.pauseInfo.TillTime.Format(time.TimeOnly))

	c.pauseNotification = &notifications.Notification{
		EventID:      "control:paused",
		Type:         nType,
		Title:        title,
		Message:      message,
		ShowOnSystem: false, // TODO: Before enabling, ensure that UI client (Tauri implementation) supports ActionTypeWebhook.
		EventData:    &c.pauseInfo,
		AvailableActions: []*notifications.Action{
			{
				Text: "Resume",
				Type: notifications.ActionTypeWebhook,
				Payload: &notifications.ActionTypeWebhookPayload{
					URL:          APIEndpointResume,
					ResultAction: "display",
				},
			},
		},
	}

	notifications.Notify(c.pauseNotification)
	c.pauseNotification.SyncWithState(c.states)
}

// updateStatesAndNotifyError updates the paused states and sends an error notification.
// No thread safety, caller must hold c.locker.
func (c *Control) updateStatesAndNotifyError(errDescription string, err error) {
	if err == nil {
		return
	}

	if errDescription == "" {
		errDescription = "Error"
	}

	// Error notification
	c.pauseNotification = &notifications.Notification{
		EventID:   "control:error",
		Type:      notifications.Error,
		Title:     errDescription,
		Message:   err.Error(),
		EventData: &c.pauseInfo,
	}
	notifications.Notify(c.pauseNotification)
	c.pauseNotification.SyncWithState(c.states)
}

func (c *Control) showNotification(title, message string) *notifications.Notification {
	n := &notifications.Notification{
		EventID: "control:status_info",
		Type:    notifications.Info,
		Title:   title,
		Message: message,
	}
	notifications.Notify(n)
	return n
}

func (c *Control) waitSPNStopped(stopTimeout time.Duration) error {
	var notification *notifications.Notification
	defer func() {
		if notification != nil {
			notification.Delete()
		}
	}()

	startTime := time.Now()
	isStopped, _ := c.instance.SPNGroup().IsStopped()
	for !isStopped {
		var err error

		time.Sleep(200 * time.Millisecond)

		if c.mgr.IsDone() || c.instance.IsShuttingDown() {
			return errors.New("shutting down")
		}

		isStopped, err = c.instance.SPNGroup().IsStopped()
		if err != nil {
			return fmt.Errorf("failed to stop SPN: %w", err)
		}
		if time.Since(startTime) > stopTimeout {
			return errors.New("timeout waiting for SPN to stop")
		}
		if notification == nil && time.Since(startTime) > time.Second {
			notification = c.showNotification("Waiting for SPN to stop...", "")
		}
		if c.cfgSpnEnabled() {
			return errors.New("SPN enabled again")
		}
	}
	return nil
}
