package core

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ghodss/yaml"

	"github.com/safing/portmaster/base/api"
	"github.com/safing/portmaster/base/config"
	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/base/notifications"
	"github.com/safing/portmaster/base/rng"
	"github.com/safing/portmaster/base/utils"
	"github.com/safing/portmaster/base/utils/debug"
	"github.com/safing/portmaster/service/compat"
	"github.com/safing/portmaster/service/process"
	"github.com/safing/portmaster/service/resolver"
	"github.com/safing/portmaster/service/status"
	"github.com/safing/portmaster/spn/captain"
)

var errInvalidReadPermission = errors.New("invalid read permission")

func registerAPIEndpoints() error {
	if err := api.RegisterEndpoint(api.Endpoint{
		Path:  "core/shutdown",
		Write: api.PermitSelf,
		// Do NOT register as belonging to the module, so that the API is available
		// when something fails during starting of this module or a dependency.
		ActionFunc:  shutdown,
		Name:        "Shut Down Portmaster",
		Description: "Shut down the Portmaster Core Service and all UI components.",
	}); err != nil {
		return err
	}

	if err := api.RegisterEndpoint(api.Endpoint{
		Path:  "core/restart",
		Write: api.PermitAdmin,
		// Do NOT register as belonging to the module, so that the API is available
		// when something fails during starting of this module or a dependency.
		ActionFunc:  restart,
		Name:        "Restart Portmaster",
		Description: "Restart the Portmaster Core Service.",
	}); err != nil {
		return err
	}

	if err := api.RegisterEndpoint(api.Endpoint{
		Path:        "debug/core",
		Read:        api.PermitAnyone,
		DataFunc:    debugInfo,
		Name:        "Get Debug Information",
		Description: "Returns network debugging information, similar to debug/info, but with system status data.",
		Parameters: []api.Parameter{{
			Method:      http.MethodGet,
			Field:       "style",
			Value:       "github",
			Description: "Specify the formatting style. The default is simple markdown formatting.",
		}},
	}); err != nil {
		return err
	}

	if err := api.RegisterEndpoint(api.Endpoint{
		Path:       "app/auth",
		Read:       api.PermitAnyone,
		StructFunc: authorizeApp,
		Name:       "Request an authentication token with a given set of permissions. The user will be prompted to either authorize or deny the request. Used for external or third-party tool integrations.",
		Parameters: []api.Parameter{
			{
				Method:      http.MethodGet,
				Field:       "app-name",
				Description: "The name of the application requesting access",
			},
			{
				Method:      http.MethodGet,
				Field:       "read",
				Description: "The requested read permission",
			},
			{
				Method:      http.MethodGet,
				Field:       "write",
				Description: "The requested write permission",
			},
			{
				Method:      http.MethodGet,
				Field:       "ttl",
				Description: "The time-to-live for the new access token. Defaults to 24h",
			},
		},
	}); err != nil {
		return err
	}

	if err := api.RegisterEndpoint(api.Endpoint{
		Path:       "app/profile",
		Read:       api.PermitUser,
		StructFunc: getMyProfile,
		Name:       "Get the ID of the calling profile",
	}); err != nil {
		return err
	}

	if err := api.RegisterEndpoint(api.Endpoint{
		Path:        "updates/check",
		WriteMethod: "POST",
		Write:       api.PermitUser,
		ActionFunc: func(ar *api.Request) (string, error) {
			module.instance.BinaryUpdates().TriggerUpdateCheck()
			module.instance.IntelUpdates().TriggerUpdateCheck()
			return "update check triggered", nil
		},
		Name: "Trigger updates check event",
	}); err != nil {
		return err
	}

	if err := api.RegisterEndpoint(api.Endpoint{
		Path:        "updates/apply",
		WriteMethod: "POST",
		Write:       api.PermitUser,
		ActionFunc: func(ar *api.Request) (string, error) {
			module.instance.BinaryUpdates().TriggerApplyUpdates()
			module.instance.IntelUpdates().TriggerApplyUpdates()
			return "upgrade triggered", nil
		},
		Name: "Trigger updates apply event",
	}); err != nil {
		return err
	}

	if err := api.RegisterEndpoint(api.Endpoint{
		Path:        "updates/from-url",
		WriteMethod: "POST",
		Write:       api.PermitAnyone,
		ActionFunc: func(ar *api.Request) (string, error) {
			err := module.instance.BinaryUpdates().UpdateFromURL(string(ar.InputData))
			if err != nil {
				return err.Error(), err
			}
			return "upgrade triggered", nil
		},
		Name: "Replace current version from the version supplied in the URL",
	}); err != nil {
		return err
	}

	if err := api.RegisterEndpoint(api.Endpoint{
		Name:        "Get Resource",
		Description: "Returns the requested resource from the update system",
		Path:        `updates/get/{artifact_path:[A-Za-z0-9/\.\-_]{1,255}}/{artifact_name:[A-Za-z0-9\.\-_]{1,255}}`,
		Read:        api.PermitUser,
		ReadMethod:  http.MethodGet,
		HandlerFunc: getUpdateResource,
	}); err != nil {
		return err
	}

	return nil
}

// shutdown shuts the Portmaster down.
func shutdown(_ *api.Request) (msg string, err error) {
	log.Warning("core: user requested shutdown via action")

	module.instance.Shutdown()
	return "shutdown initiated", nil
}

// restart restarts the Portmaster.
func restart(_ *api.Request) (msg string, err error) {
	log.Info("core: user requested restart via action")

	// Trigger restart
	module.instance.Restart()

	return "restart initiated", nil
}

func getUpdateResource(w http.ResponseWriter, r *http.Request) {
	// Get identifier from URL.
	var identifier string
	if ar := api.GetAPIRequest(r); ar != nil {
		identifier = ar.URLVars["artifact_name"]
	}
	if identifier == "" {
		http.Error(w, "no resource specified", http.StatusBadRequest)
		return
	}

	// Get resource.
	artifact, err := module.instance.BinaryUpdates().GetFile(identifier)
	if err != nil {
		intelArtifact, intelErr := module.instance.IntelUpdates().GetFile(identifier)
		if intelErr == nil {
			artifact = intelArtifact
		} else {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
	}

	// Open file for reading.
	file, err := os.Open(artifact.Path())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer file.Close() //nolint:errcheck,gosec

	// Assign file to reader
	var reader io.Reader = file

	// Add version and hash to header.
	if artifact.Version != "" {
		w.Header().Set("Resource-Version", artifact.Version)
	}
	if artifact.SHA256 != "" {
		w.Header().Set("Resource-SHA256", artifact.SHA256)
	}

	// Set Content-Type.
	contentType, _ := utils.MimeTypeByExtension(filepath.Ext(artifact.Path()))
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
}

// debugInfo returns the debugging information for support requests.
func debugInfo(ar *api.Request) (data []byte, err error) {
	// Create debug information helper.
	di := new(debug.Info)
	di.Style = ar.Request.URL.Query().Get("style")

	// Add debug information.

	// Very basic information at the start.
	di.AddVersionInfo()
	di.AddPlatformInfo(ar.Context())

	// Unexpected logs.
	di.AddLastUnexpectedLogs()

	// Status Information from various modules.
	status.AddToDebugInfo(di)
	captain.AddToDebugInfo(di)
	resolver.AddToDebugInfo(di)
	config.AddToDebugInfo(di)

	// Detailed information.
	AddVersionsToDebugInfo(di)
	compat.AddToDebugInfo(di)
	module.instance.AddWorkerInfoToDebugInfo(di)
	di.AddGoroutineStack()

	// Return data.
	return di.Bytes(), nil
}

// getSavePermission returns the requested api.Permission from p.
// It only allows "user" and "admin" as external processes should
// never be able to request "self".
func getSavePermission(p string) api.Permission {
	switch p {
	case "user":
		return api.PermitUser
	case "admin":
		return api.PermitAdmin
	default:
		return api.NotSupported
	}
}

func getMyProfile(ar *api.Request) (interface{}, error) {
	proc, err := process.GetProcessByRequestOrigin(ar)
	if err != nil {
		return nil, err
	}

	localProfile := proc.Profile().LocalProfile()

	return map[string]interface{}{
		"profile": localProfile.ID,
		"source":  localProfile.Source,
		"name":    localProfile.Name,
	}, nil
}

func authorizeApp(ar *api.Request) (interface{}, error) {
	appName := ar.Request.URL.Query().Get("app-name")
	readPermStr := ar.Request.URL.Query().Get("read")
	writePermStr := ar.Request.URL.Query().Get("write")

	ttl := time.Hour * 24
	if ttlStr := ar.Request.URL.Query().Get("ttl"); ttlStr != "" {
		var err error
		ttl, err = time.ParseDuration(ttlStr)
		if err != nil {
			return nil, err
		}
	}

	// convert the requested read and write permissions to their api.Permission
	// value. This ensures only "user" or "admin" permissions can be requested.
	if getSavePermission(readPermStr) <= api.NotSupported {
		return nil, errInvalidReadPermission
	}
	if getSavePermission(writePermStr) <= api.NotSupported {
		return nil, errInvalidReadPermission
	}

	proc, err := process.GetProcessByRequestOrigin(ar)
	if err != nil {
		return nil, fmt.Errorf("failed to identify requesting process: %w", err)
	}

	n := notifications.Notification{
		Type:         notifications.Prompt,
		EventID:      "core:authorize-app-" + time.Now().String(),
		Title:        "An app requests access to the Portmaster",
		Message:      "Allow " + appName + " (" + proc.Profile().LocalProfile().Name + ") to query and modify the Portmaster?\n\nBinary: " + proc.Path,
		ShowOnSystem: true,
		Expires:      time.Now().Add(time.Minute).Unix(),
		AvailableActions: []*notifications.Action{
			{
				ID:   "allow",
				Text: "Authorize",
			},
			{
				ID:   "deny",
				Text: "Deny",
			},
		},
	}

	ch := make(chan string)

	validUntil := time.Now().Add(ttl)

	n.SetActionFunction(func(ctx context.Context, n *notifications.Notification) error {
		n.Lock()
		defer n.Unlock()

		if n.SelectedActionID != "allow" {
			close(ch)
			return nil
		}

		keys := config.Concurrent.GetAsStringArray(api.CfgAPIKeys, []string{})()

		newKeyData, err := rng.Bytes(8)
		if err != nil {
			return err
		}

		newKeyHex := hex.EncodeToString(newKeyData)

		query := url.Values{
			"read":    []string{readPermStr},
			"write":   []string{writePermStr},
			"expires": []string{validUntil.Format(time.RFC3339)},
		}

		keys = append(keys, fmt.Sprintf("%s?%s", newKeyHex, query.Encode()))

		if err := config.SetConfigOption(api.CfgAPIKeys, keys); err != nil {
			return err
		}

		ch <- newKeyHex

		return nil
	})

	n.Save()

	select {
	case key := <-ch:
		if len(key) == 0 {
			return nil, errors.New("access denied")
		}

		return map[string]interface{}{
			"key":        key,
			"validUntil": validUntil,
		}, nil
	case <-ar.Context().Done():
		return nil, errors.New("timeout")
	}
}
