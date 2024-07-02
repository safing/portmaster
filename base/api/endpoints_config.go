package api

import (
	"github.com/safing/portmaster/base/config"
)

func registerConfigEndpoints() error {
	if err := RegisterEndpoint(Endpoint{
		Path:        "config/options",
		Read:        PermitAnyone,
		MimeType:    MimeTypeJSON,
		StructFunc:  listConfig,
		Name:        "Export Configuration Options",
		Description: "Returns a list of all registered configuration options and their metadata. This does not include the current active or default settings.",
	}); err != nil {
		return err
	}

	return nil
}

func listConfig(ar *Request) (i interface{}, err error) {
	return config.ExportOptions(), nil
}
