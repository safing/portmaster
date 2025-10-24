package control

import (
	"net/http"

	"github.com/safing/portmaster/base/api"
)

type pauseRequestParams struct {
	Duration int  `json:"duration"` // Duration in seconds
	OnlySPN  bool `json:"onlySPN"`  // Whether to pause only the SPN service
}

func (c *Control) registerAPIEndpoints() error {

	if err := api.RegisterEndpoint(api.Endpoint{
		Path:        "control/pause",
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
		Path:        "control/resume",
		Write:       api.PermitAdmin,
		ActionFunc:  c.handleResume,
		Name:        "Resume Portmaster",
		Description: "Resume the Portmaster Core Service.",
	}); err != nil {
		return err
	}

	return nil
}
