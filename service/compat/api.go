package compat

import (
	"github.com/safing/portmaster/base/api"
)

func registerAPIEndpoints() error {
	if err := api.RegisterEndpoint(api.Endpoint{
		Path:        "compat/self-check",
		Read:        api.PermitUser,
		ActionFunc:  selfcheckViaAPI,
		Name:        "Run Integration Self-Check",
		Description: "Runs a couple integration self-checks in order to see if the system integration works.",
	}); err != nil {
		return err
	}

	return nil
}

func selfcheckViaAPI(ar *api.Request) (msg string, err error) {
	_, err = selfcheck(ar.Context())
	if err != nil {
		return "", err
	}

	return "self-check successful", nil
}
