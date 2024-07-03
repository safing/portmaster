package sync

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/safing/portmaster/base/api"
	"github.com/safing/portmaster/base/config"
	"github.com/safing/portmaster/service/profile"
)

// SettingsExport holds an export of settings.
type SettingsExport struct {
	Type Type `json:"type" yaml:"type"`

	Config map[string]any `json:"config" yaml:"config"`
}

// SettingsImportRequest is a request to import settings.
type SettingsImportRequest struct {
	ImportRequest `json:",inline" yaml:",inline"`

	// Reset all settings of target before import.
	// The ImportResult also reacts to this flag and correctly reports whether
	// any settings would be replaced or deleted.
	Reset bool `json:"reset" yaml:"reset"`

	// AllowUnknown allows the import of unknown settings.
	// Otherwise, attempting to import an unknown setting will result in an error.
	AllowUnknown bool `json:"allowUnknown" yaml:"allowUnknown"`

	Export *SettingsExport `json:"export" yaml:"export"`
}

func registerSettingsAPI() error {
	if err := api.RegisterEndpoint(api.Endpoint{
		Name:        "Export Settings",
		Description: "Exports settings in a share-able format.",
		Path:        "sync/settings/export",
		Read:        api.PermitAdmin,
		Write:       api.PermitAdmin,
		Parameters: []api.Parameter{{
			Method:      http.MethodGet,
			Field:       "from",
			Description: "Specify where to export from.",
		}, {
			Method:      http.MethodGet,
			Field:       "key",
			Description: "Optionally select a single setting to export. Repeat to export selection.",
		}},
		DataFunc: handleExportSettings,
	}); err != nil {
		return err
	}

	if err := api.RegisterEndpoint(api.Endpoint{
		Name:        "Import Settings",
		Description: "Imports settings from the share-able format.",
		Path:        "sync/settings/import",
		Read:        api.PermitAdmin,
		Write:       api.PermitAdmin,
		Parameters: []api.Parameter{{
			Method:      http.MethodPost,
			Field:       "to",
			Description: "Specify where to import to.",
		}, {
			Method:      http.MethodPost,
			Field:       "validate",
			Description: "Validate only.",
		}, {
			Method:      http.MethodPost,
			Field:       "reset",
			Description: "Replace all existing settings.",
		}, {
			Method:      http.MethodPost,
			Field:       "allowUnknown",
			Description: "Allow importing of unknown values.",
		}},
		StructFunc: handleImportSettings,
	}); err != nil {
		return err
	}

	return nil
}

func handleExportSettings(ar *api.Request) (data []byte, err error) {
	var request *ExportRequest

	// Get parameters.
	q := ar.URL.Query()
	if len(q) > 0 {
		request = &ExportRequest{
			From: q.Get("from"),
			Keys: q["key"], // Get []string by direct map access.
		}
	} else {
		request = &ExportRequest{}
		if err := json.Unmarshal(ar.InputData, request); err != nil {
			return nil, fmt.Errorf("%w: failed to parse export request: %w", ErrExportFailed, err)
		}
	}

	// Check parameters.
	if request.From == "" {
		return nil, errors.New("missing parameters")
	}

	// Export.
	export, err := ExportSettings(request.From, request.Keys)
	if err != nil {
		return nil, err
	}

	return serializeExport(export, ar)
}

func handleImportSettings(ar *api.Request) (any, error) {
	var request *SettingsImportRequest

	// Get parameters.
	q := ar.URL.Query()
	if len(q) > 0 {
		request = &SettingsImportRequest{
			ImportRequest: ImportRequest{
				Target:       q.Get("to"),
				ValidateOnly: q.Has("validate"),
				RawExport:    string(ar.InputData),
				RawMime:      ar.Header.Get("Content-Type"),
			},
			Reset:        q.Has("reset"),
			AllowUnknown: q.Has("allowUnknown"),
		}
	} else {
		request = &SettingsImportRequest{}
		if err := json.Unmarshal(ar.InputData, request); err != nil {
			return nil, fmt.Errorf("%w: failed to parse import request: %w", ErrInvalidImportRequest, err)
		}
	}

	// Check if we need to parse the export.
	switch {
	case request.Export != nil && request.RawExport != "":
		return nil, fmt.Errorf("%w: both Export and RawExport are defined", ErrInvalidImportRequest)
	case request.RawExport != "":
		// Parse export.
		export := &SettingsExport{}
		if err := parseExport(&request.ImportRequest, export); err != nil {
			return nil, err
		}
		request.Export = export
	case request.Export != nil:
		// Export is already parsed.
	default:
		return nil, ErrInvalidImportRequest
	}

	// Import.
	return ImportSettings(request)
}

// ExportSettings exports the global settings.
func ExportSettings(from string, keys []string) (*SettingsExport, error) {
	var settings map[string]any
	if from == ExportTargetGlobal {
		// Collect all changed global settings.
		settings = make(map[string]any)
		_ = config.ForEachOption(func(option *config.Option) error {
			v := option.UserValue()
			if v != nil {
				settings[option.Key] = v
			}
			return nil
		})
	} else {
		r, err := db.Get(profile.ProfilesDBPath + from)
		if err != nil {
			return nil, fmt.Errorf("%w: failed to find profile: %w", ErrTargetNotFound, err)
		}
		p, err := profile.EnsureProfile(r)
		if err != nil {
			return nil, fmt.Errorf("%w: failed to load profile: %w", ErrExportFailed, err)
		}
		settings = config.Flatten(p.Config)
	}

	// Only extract some setting keys, if wanted.
	if len(keys) > 0 {
		selection := make(map[string]any, len(keys))
		for _, key := range keys {
			if v, ok := settings[key]; ok {
				selection[key] = v
			}
		}
		settings = selection
	}

	// Check if there any changed settings.
	if len(settings) == 0 {
		return nil, ErrUnchanged
	}

	// Expand config to hierarchical form.
	settings = config.Expand(settings)

	return &SettingsExport{
		Type:   TypeSettings,
		Config: settings,
	}, nil
}

// ImportSettings imports the global settings.
func ImportSettings(r *SettingsImportRequest) (*ImportResult, error) {
	// Check import.
	if r.Export.Type != TypeSettings {
		return nil, ErrMismatch
	}
	// Flatten config.
	settings := config.Flatten(r.Export.Config)

	// Check settings.
	result, globalOnlySettingFound, err := checkSettings(settings)
	if err != nil {
		return nil, err
	}
	if result.ContainsUnknown && !r.AllowUnknown && !r.ValidateOnly {
		return nil, fmt.Errorf("%w: the export contains unknown settings", ErrInvalidImportRequest)
	}

	// Import global settings.
	if r.Target == ExportTargetGlobal {
		// Stop here if we are only validating.
		if r.ValidateOnly {
			return result, nil
		}

		// Import to global config.
		vErrs, restartRequired := config.ReplaceConfig(settings)
		if len(vErrs) > 0 {
			s := make([]string, 0, len(vErrs))
			for _, err := range vErrs {
				s = append(s, err.Error())
			}
			return nil, fmt.Errorf(
				"%w: the supplied configuration could not be applied:\n%s",
				ErrImportFailed,
				strings.Join(s, "\n"),
			)
		}

		// Save new config to disk.
		err := config.SaveConfig()
		if err != nil {
			return nil, fmt.Errorf("failed to save config: %w", err)
		}

		result.RestartRequired = restartRequired
		return result, nil
	}

	// Check if a setting is settable per app.
	if globalOnlySettingFound {
		return nil, fmt.Errorf("%w: export contains settings that cannot be set per app", ErrNotSettablePerApp)
	}

	// Get and load profile.
	rec, err := db.Get(profile.ProfilesDBPath + r.Target)
	if err != nil {
		return nil, fmt.Errorf("%w: failed to find profile: %w", ErrTargetNotFound, err)
	}
	p, err := profile.EnsureProfile(rec)
	if err != nil {
		return nil, fmt.Errorf("%w: failed to load profile: %w", ErrImportFailed, err)
	}

	// Stop here if we are only validating.
	if r.ValidateOnly {
		return result, nil
	}

	// Import settings into profile.
	if r.Reset {
		p.Config = config.Expand(settings)
	} else {
		for k, v := range settings {
			config.PutValueIntoHierarchicalConfig(p.Config, k, v)
		}
	}

	// Mark profile as edited by user.
	p.LastEdited = time.Now().Unix()

	// Save profile back to db.
	err = p.Save()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to save profile: %w", ErrImportFailed, err)
	}

	return result, nil
}

func checkSettings(settings map[string]any) (result *ImportResult, globalOnlySettingFound bool, err error) {
	result = &ImportResult{}

	// Validate config and gather some metadata.
	var checked int
	err = config.ForEachOption(func(option *config.Option) error {
		// Check if any setting is set.
		// TODO: Fix this - it only checks for global settings.
		// if r.Reset && option.IsSetByUser() {
		// 	result.ReplacesExisting = true
		// }

		newValue, ok := settings[option.Key]
		if ok {
			checked++

			// Validate the new value.
			if err := option.ValidateValue(newValue); err != nil {
				return fmt.Errorf("%w: configuration value for %s is invalid: %w", ErrInvalidSettingValue, option.Key, err)
			}

			// Collect metadata.
			if option.RequiresRestart {
				result.RestartRequired = true
			}
			// TODO: Fix this - it only checks for global settings.
			// if !r.Reset && option.IsSetByUser() {
			// 	result.ReplacesExisting = true
			// }
			if !option.AnnotationEquals(config.SettablePerAppAnnotation, true) {
				globalOnlySettingFound = true
			}
		}
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	if checked < len(settings) {
		result.ContainsUnknown = true
	}

	return result, globalOnlySettingFound, nil
}
