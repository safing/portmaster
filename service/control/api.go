package control

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/safing/portmaster/base/api"
)

const (
	APIEndpointPause  = "control/pause"
	APIEndpointResume = "control/resume"
)

type pauseRequestParams struct {
	Duration int  `json:"duration"` // Duration in seconds
	OnlySPN  bool `json:"onlySPN"`  // Whether to pause only the SPN service
}

func (c *Control) registerAPIEndpoints() error {

	if err := api.RegisterEndpoint(api.Endpoint{
		Path:        APIEndpointPause,
		Write:       api.PermitAdmin,
		ActionFunc:  c.handlePause,
		Name:        "Pause Portmaster",
		Description: "Pause the Portmaster Core Service.",
		Parameters: []api.Parameter{
			{
				Method:      http.MethodPost,
				Field:       "duration",
				Description: "Specify the duration to pause the service in seconds.",
			},
			{
				Method:      http.MethodPost,
				Field:       "onlySPN",
				Value:       "false",
				Description: "Specify whether to pause only the SPN service.",
			}},
	}); err != nil {
		return err
	}

	if err := api.RegisterEndpoint(api.Endpoint{
		Path:        APIEndpointResume,
		Write:       api.PermitAdmin,
		ActionFunc:  c.handleResume,
		Name:        "Resume Portmaster",
		Description: "Resume the Portmaster Core Service.",
	}); err != nil {
		return err
	}

	return nil
}

func (c *Control) handlePause(r *api.Request) (msg string, err error) {
	params := pauseRequestParams{}
	if r.InputData != nil {
		if err := json.Unmarshal(r.InputData, &params); err != nil {
			return "Bad Request: invalid input data", err
		}
	}

	if params.OnlySPN {
		c.mgr.Info(fmt.Sprintf("Received SPN Pause(%v) action request ", params.Duration))
	} else {
		c.mgr.Info(fmt.Sprintf("Received Pause(%v) action request ", params.Duration))
	}

	if err := c.pause(time.Duration(params.Duration)*time.Second, params.OnlySPN); err != nil {
		return "Failed to pause", err
	}
	return "Pause initiated", nil
}

func (c *Control) handleResume(_ *api.Request) (msg string, err error) {
	c.mgr.Info("Received Resume action request")

	if err := c.resume(); err != nil {
		return "Failed to resume", err
	}
	return "Resume initiated", nil
}
