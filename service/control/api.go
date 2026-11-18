package control

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/safing/portmaster/base/api"
)

const (
	APIEndpointPause  = "control/pause"
	APIEndpointResume = "control/resume"
)

// errOnlySPNNotSupported is returned when a client asks to pause only the SPN.
// This build has no SPN, so such a request is rejected instead of pausing the
// whole interception, which the client did not ask for.
var errOnlySPNNotSupported = errors.New("pausing only the SPN is not supported: this build has no SPN")

type pauseRequestParams struct {
	Duration int `json:"duration"` // Duration in seconds
	// OnlySPN is still accepted for compatibility with older clients. A value
	// of false is ignored, true is rejected with errOnlySPNNotSupported.
	OnlySPN bool `json:"onlySPN"`
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
		return "Bad Request: " + errOnlySPNNotSupported.Error(), errOnlySPNNotSupported
	}

	c.mgr.Info(fmt.Sprintf("Received Pause(%v) action request ", params.Duration))

	err = c.pause(time.Duration(params.Duration) * time.Second)
	if err != nil {
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
