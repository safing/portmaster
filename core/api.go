package core

import (
	"context"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/safing/portbase/api"
	"github.com/safing/portbase/config"
	"github.com/safing/portbase/log"
	"github.com/safing/portbase/modules"
	"github.com/safing/portbase/notifications"
	"github.com/safing/portbase/rng"
	"github.com/safing/portbase/utils/debug"
	"github.com/safing/portmaster/compat"
	"github.com/safing/portmaster/process"
	"github.com/safing/portmaster/resolver"
	"github.com/safing/portmaster/status"
	"github.com/safing/portmaster/updates"
	"github.com/safing/spn/captain"
)

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
		BelongsTo:   module,
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
		BelongsTo:  module,
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
		BelongsTo:  module,
		StructFunc: getMyProfile,
		Name:       "Get the ID of the calling profile",
	}); err != nil {
		return err
	}

	return nil
}

// shutdown shuts the Portmaster down.
func shutdown(_ *api.Request) (msg string, err error) {
	log.Warning("core: user requested shutdown via action")

	// Do not run in worker, as this would block itself here.
	go modules.Shutdown() //nolint:errcheck

	return "shutdown initiated", nil
}

// restart restarts the Portmaster.
func restart(_ *api.Request) (msg string, err error) {
	log.Info("core: user requested restart via action")

	// Let the updates module handle restarting.
	updates.RestartNow()

	return "restart initiated", nil
}

// debugInfo returns the debugging information for support requests.
func debugInfo(ar *api.Request) (data []byte, err error) {
	// Create debug information helper.
	di := new(debug.Info)
	di.Style = ar.Request.URL.Query().Get("style")

	// Add debug information.
	di.AddVersionInfo()
	di.AddPlatformInfo(ar.Context())
	status.AddToDebugInfo(di)
	config.AddToDebugInfo(di)
	resolver.AddToDebugInfo(di)
	captain.AddToDebugInfo(di)
	compat.AddToDebugInfo(di)
	di.AddLastReportedModuleError()
	di.AddLastUnexpectedLogs()
	updates.AddToDebugInfo(di)
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
		return nil, fmt.Errorf("invalid read permission")
	}
	if getSavePermission(writePermStr) <= api.NotSupported {
		return nil, fmt.Errorf("invalid read permission")
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
		Expires:      time.Now().Add(time.Minute).UnixNano(),
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
			return nil, fmt.Errorf("access denied")
		}

		return map[string]interface{}{
			"key":        key,
			"validUntil": validUntil,
		}, nil
	case <-ar.Context().Done():
		return nil, fmt.Errorf("timeout")
	}
}
