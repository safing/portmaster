package updates

import (
	"bytes"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/ghodss/yaml"

	"github.com/safing/portmaster/base/api"
	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/base/utils"
)

const (
	apiPathCheckForUpdates = "updates/check"
)

func registerAPIEndpoints() error {
	if err := api.RegisterEndpoint(api.Endpoint{
		Name:        "Check for Updates",
		Description: "Checks if new versions are available. If automatic updates are enabled, they are also downloaded and applied.",
		Parameters: []api.Parameter{{
			Method:      http.MethodPost,
			Field:       "download",
			Value:       "",
			Description: "Force downloading and applying of all updates, regardless of auto-update settings.",
		}},
		Path:  apiPathCheckForUpdates,
		Write: api.PermitUser,
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
	}); err != nil {
		return err
	}

	if err := api.RegisterEndpoint(api.Endpoint{
		Name:        "Get Resource",
		Description: "Returns the requested resource from the udpate system",
		Path:        `updates/get/{identifier:[A-Za-z0-9/\.\-_]{1,255}}`,
		Read:        api.PermitUser,
		ReadMethod:  http.MethodGet,
		HandlerFunc: func(w http.ResponseWriter, r *http.Request) {
			// Get identifier from URL.
			var identifier string
			if ar := api.GetAPIRequest(r); ar != nil {
				identifier = ar.URLVars["identifier"]
			}
			if identifier == "" {
				http.Error(w, "no resource speicified", http.StatusBadRequest)
				return
			}

			// Get resource.
			resource, err := registry.GetFile(identifier)
			if err != nil {
				http.Error(w, err.Error(), http.StatusNotFound)
				return
			}

			// Open file for reading.
			file, err := os.Open(resource.Path())
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			defer file.Close() //nolint:errcheck,gosec

			// Assign file to reader
			var reader io.Reader = file

			// Add version to header.
			w.Header().Set("Resource-Version", resource.Version())

			// Set Content-Type.
			contentType, _ := utils.MimeTypeByExtension(filepath.Ext(resource.Path()))
			w.Header().Set("Content-Type", contentType)

			// Check if the content type may be returned.
			accept := r.Header.Get("Accept")
			if accept != "" {
				mimeTypes := strings.Split(accept, ",")
				// First, clean mime types.
				for i, mimeType := range mimeTypes {
					mimeType = strings.TrimSpace(mimeType)
					mimeType, _, _ = strings.Cut(mimeType, ";")
					mimeTypes[i] = mimeType
				}
				// Second, check if we may return anything.
				var acceptsAny bool
				for _, mimeType := range mimeTypes {
					switch mimeType {
					case "*", "*/*":
						acceptsAny = true
					}
				}
				// Third, check if we can convert.
				if !acceptsAny {
					var converted bool
					sourceType, _, _ := strings.Cut(contentType, ";")
				findConvertiblePair:
					for _, mimeType := range mimeTypes {
						switch {
						case sourceType == "application/yaml" && mimeType == "application/json":
							yamlData, err := io.ReadAll(reader)
							if err != nil {
								http.Error(w, err.Error(), http.StatusInternalServerError)
								return
							}
							jsonData, err := yaml.YAMLToJSON(yamlData)
							if err != nil {
								http.Error(w, err.Error(), http.StatusInternalServerError)
								return
							}
							reader = bytes.NewReader(jsonData)
							converted = true
							break findConvertiblePair
						}
					}

					// If we could not convert to acceptable format, return an error.
					if !converted {
						http.Error(w, "conversion to requested format not supported", http.StatusNotAcceptable)
						return
					}
				}
			}

			// Write file.
			w.WriteHeader(http.StatusOK)
			if r.Method != http.MethodHead {
				_, err = io.Copy(w, reader)
				if err != nil {
					log.Errorf("updates: failed to serve resource file: %s", err)
					return
				}
			}
		},
	}); err != nil {
		return err
	}

	return nil
}
