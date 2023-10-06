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
			if err := TriggerUpdate(false); err != nil {
				return "", err
			}
			return "triggered update check", nil
		},
		Name:        "Check for Updates",
		Description: "Checks if new versions are available and downloads and applies them, if automatic updates are enabled.",
	})
}
