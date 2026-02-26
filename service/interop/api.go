package interop

import (
	"github.com/safing/portmaster/base/api"
)

const (
	APIEndpointPing = "interop/ping"
)

type pingParams struct {
	Message bool `json:"message"`
}

func (i *Interoperability) registerAPIEndpoints() error {
	if err := api.RegisterEndpoint(api.Endpoint{
		Path:        APIEndpointPing,
		Read:        api.PermitAnyone,
		Write:       api.PermitAnyone,
		ActionFunc:  i.handlePing,
		Name:        "Interoperability: Ping Portmaster",
		Description: "Ping the Portmaster Core Service for interoperability checks.",
	}); err != nil {
		return err
	}

	return nil
}

func (c *Interoperability) handlePing(r *api.Request) (msg string, err error) {
	// Received ping, possibly from IVPN client
	// Try to connect to IVPN client if not already connected

	for _, im := range c.interopModules {
		if err := im.PingHandler(); err != nil {
			c.mgr.Warn("Failed to handle ping for interoperability module: " + err.Error())
		}
	}

	return "", nil
}
