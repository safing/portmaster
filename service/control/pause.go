package control

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/safing/portmaster/base/api"
	"github.com/safing/portmaster/base/config"
	"github.com/safing/portmaster/service/mgr"
)

func (c *Control) handlePause(r *api.Request) (msg string, err error) {
	params := pauseRequestParams{}
	if r.InputData != nil {
		if err := json.Unmarshal(r.InputData, &params); err != nil {
			return "Bad Request: invalid input data", err
		}
	}

	if params.OnlySPN {
		c.mgr.Info(fmt.Sprintf("Received SPN PAUSE(%v) action request ", params.Duration))
	} else {
		c.mgr.Info(fmt.Sprintf("Received PAUSE(%v) action request ", params.Duration))
	}

	if err := c.impl_pause(time.Duration(params.Duration)*time.Second, params.OnlySPN); err != nil {
		return "Failed to pause", err
	}
	return "Pause initiated", nil
}

func (c *Control) handleResume(_ *api.Request) (msg string, err error) {
	c.mgr.Info("Received RESUME action request")
	if err := c.impl_resume(); err != nil {
		return "Failed to resume", err
	}
	return "Resume initiated", nil
}

func (c *Control) impl_pause(duration time.Duration, onlySPN bool) (retErr error) {
	c.locker.Lock()
	defer c.locker.Unlock()

	if duration <= 0 {
		return errors.New(logPrefix + "invalid pause duration")
	}

	if onlySPN {
		if c.isPaused {
			return errors.New(logPrefix + "cannot pause SPN separately when core is paused")
		}
		if !c.isPausedSPN && !c.instance.SPNGroup().Ready() {
			return errors.New(logPrefix + "cannot pause SPN when it is not running")
		}
	}

	c.stopResumeWorker()
	defer func() {
		if retErr == nil {
			// start new resume worker (with new duration) if no error
			c.startResumeWorker(duration)
		}
	}()

	if !c.isPausedSPN {
		if c.instance.SPNGroup().Ready() {

			// TODO: the 'pause' state must not make permanent config changes.
			// Consider possibility to not store permanent config changes.
			// E.g. SPN enabled -> pause SPN -> restart PC/Portmaster -> SPN should be enabled again.
			enabled := config.GetAsBool("spn/enable", false)
			if enabled() {
				config.SetConfigOption("spn/enable", false)
			}

			// Alternatively, we could directly stop SPN here:
			//  if c.instance.IsShuttingDown() {
			//	  c.mgr.Warn("Skipping pause during shutdown")
			//	  return nil
			//  }
			//	err := c.instance.SPNGroup().Stop()
			//	if err != nil {
			//		return err
			//	}
			//	c.mgr.Info("SPN paused")

			c.isPausedSPN = true
		}
	}

	if onlySPN {
		return nil
	}
	if c.isPaused {
		return nil
	}

	modulesToResume := []mgr.Module{
		c.instance.Compat(),
		c.instance.Interception(),
	}
	for _, m := range modulesToResume {
		if err := m.Stop(); err != nil {
			return err
		}
	}

	c.mgr.Info("interception paused")
	c.isPaused = true

	return nil
}

func (c *Control) impl_resume() error {
	c.locker.Lock()
	defer c.locker.Unlock()

	c.stopResumeWorker()

	if c.isPausedSPN {

		// TODO: consider using event to handle  "spn/enable" changes:
		//	 	module.instance.Config().EventConfigChange
		enabled := config.GetAsBool("spn/enable", false)
		if !enabled() {
			config.SetConfigOption("spn/enable", true)
		}

		// Alternatively, we could directly start SPN here:
		//  if c.instance.IsShuttingDown() {
		// 	 c.mgr.Warn("Skipping resume during shutdown")
		// 	 return nil
		//  }
		//	if !c.instance.SPNGroup().Ready() {
		//		err := c.instance.SPNGroup().Start()
		//		if err != nil {
		//			return err
		//		}
		//		c.mgr.Info("SPN resumed")
		//	}

		c.isPausedSPN = false
	}

	if c.isPaused {
		modulesToResume := []mgr.Module{
			c.instance.Interception(),
			c.instance.Compat(),
		}

		for _, m := range modulesToResume {
			if err := m.Start(); err != nil {
				return err
			}
		}
		c.mgr.Info("interception resumed")
		c.isPaused = false
	}

	return nil
}

// stopResumeWorker stops any existing resume worker.
// No thread safety, caller must hold c.locker.
func (c *Control) stopResumeWorker() {
	c.pauseStartTime = time.Time{}
	c.pauseDuration = 0

	if c.pauseWorker != nil {
		c.pauseWorker.Stop()
		c.pauseWorker = nil
	}
}

// startResumeWorker starts a worker that will resume normal operation after the specified duration.
// No thread safety, caller must hold c.locker.
func (c *Control) startResumeWorker(duration time.Duration) {
	c.pauseStartTime = time.Now()
	c.pauseDuration = duration

	c.mgr.Info(fmt.Sprintf("Scheduling resume in %v", duration))

	c.pauseWorker = c.mgr.NewWorkerMgr(
		fmt.Sprintf("resume in %v", duration),
		func(wc *mgr.WorkerCtx) error {
			wc.Info("Resuming...")
			return c.impl_resume()
		},
		nil).Delay(duration)
}
