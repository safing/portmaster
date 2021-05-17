package updates

import (
	"github.com/safing/portbase/api"
)

const (
	apiPathCheckForUpdates = "updates/check"
)

func registerAPIEndpoints() error {
	return api.RegisterEndpoint(api.Endpoint{
		Path:      apiPathCheckForUpdates,
		Write:     api.PermitUser,
		BelongsTo: module,
		ActionFunc: func(_ *api.Request) (msg string, err error) {
			if err := TriggerUpdate(true); err != nil {
				return "", err
			}
			return "triggered update check", nil
		},
		Name:        "Check for Updates",
		Description: "Triggers checking for updates.",
	})
}
