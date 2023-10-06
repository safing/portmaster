package updates

import (
	"net/http"

	"github.com/safing/portbase/api"
)

const (
	apiPathCheckForUpdates = "updates/check"
)

func registerAPIEndpoints() error {
	return api.RegisterEndpoint(api.Endpoint{
		Name:        "Check for Updates",
		Description: "Checks if new versions are available. If automatic updates are enabled, they are also downloaded and applied.",
		Parameters: []api.Parameter{{
			Method:      http.MethodPost,
			Field:       "download",
			Value:       "",
			Description: "Force downloading and applying of all updates, regardless of auto-update settings.",
		}},
		Path:      apiPathCheckForUpdates,
		Write:     api.PermitUser,
		BelongsTo: module,
		ActionFunc: func(r *api.Request) (msg string, err error) {
			// Check if we should also download regardless of settings.
			downloadAll := r.URL.Query().Has("download")

			// Trigger update task.
			err = TriggerUpdate(true, downloadAll)
			if err != nil {
				return "", err
			}

			// Report how we triggered.
			if downloadAll {
				return "downloading all updates...", nil
			}
			return "checking for updates...", nil
		},
	})
}
