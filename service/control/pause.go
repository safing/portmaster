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

	spn_enabled := config.GetAsBool("spn/enable", false)
	if onlySPN {
		if c.pauseInfo.Interception {
			return errors.New("cannot pause SPN separately when core is paused")
		}
		// If SPN is not running and not already paused, cannot pause it or change pause duration.
		if !spn_enabled() && !c.pauseInfo.SPN {
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
		if spn_enabled() {
			// TODO: the 'pause' state must not make permanent config changes.
			// Consider possibility to not store permanent config changes.
			// E.g. SPN enabled -> pause SPN -> restart PC/Portmaster -> SPN should be enabled again.
			config.SetConfigOption("spn/enable", false)
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
		// update states after resume attempt
		c.updateStatesAndNotify()
		// log error if resume failed
		if retErr != nil {
			c.mgr.Error("Failed to resume: " + retErr.Error())
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
		enabled := config.GetAsBool("spn/enable", false)
		if !enabled() {
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
	c.pauseInfo.TillTime = time.Now().Add(duration)

	resumerWorkerFunc := func(wc *mgr.WorkerCtx) error {
		wc.Info(fmt.Sprintf("Scheduling resume in %v", duration))

		// Subscribe to config changes to detect SPN enable.
		cfgChangeEvt := c.instance.Config().EventConfigChange.Subscribe("control: spn enable check", 10)
		// Make sure to cancel subscription when worker stops.
		defer cfgChangeEvt.Cancel()

		for {
			select {
			case <-wc.Ctx().Done():
				return nil
			case <-cfgChangeEvt.Events():
				spnEnabled := config.GetAsBool("spn/enable", false)
				if spnEnabled() {
					wc.Info("SPN enabled by user, resuming...")
					return c.resume()
				}
			case <-time.After(duration):
				wc.Info("Resuming...")
				return c.resume()
			}
		}
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
	message := fmt.Sprintf("%s till %v", title, c.pauseInfo.TillTime.Format(time.TimeOnly))

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
