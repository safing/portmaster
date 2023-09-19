package sync

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/ghodss/yaml"

	"github.com/safing/portbase/api"
	"github.com/safing/portbase/config"
	"github.com/safing/portmaster/profile"
)

// SingleSettingExport holds an export of a single setting.
type SingleSettingExport struct {
	Type Type   `json:"type"` // Must be TypeSingleSetting
	ID   string `json:"id"`   // Settings Key

	Value any `json:"value"`
}

// SingleSettingImportRequest is a request to import a single setting.
type SingleSettingImportRequest struct {
	ImportRequest `json:",inline"`

	Export *SingleSettingExport `json:"export"`
}

func registerSingleSettingAPI() error {
	if err := api.RegisterEndpoint(api.Endpoint{
		Name:        "Export Single Setting",
		Description: "Exports a single setting in a share-able format.",
		Path:        "sync/single-setting/export",
		Read:        api.PermitAdmin,
		Write:       api.PermitAdmin,
		Parameters: []api.Parameter{{
			Method:      http.MethodGet,
			Field:       "from",
			Description: "Specify where to export from.",
		}, {
			Method:      http.MethodGet,
			Field:       "key",
			Description: "Specify which settings key to export.",
		}},
		BelongsTo: module,
		DataFunc:  handleExportSingleSetting,
	}); err != nil {
		return err
	}

	if err := api.RegisterEndpoint(api.Endpoint{
		Name:        "Import Single Setting",
		Description: "Imports a single setting from the share-able format.",
		Path:        "sync/single-setting/import",
		Read:        api.PermitAdmin,
		Write:       api.PermitAdmin,
		Parameters: []api.Parameter{{
			Method:      http.MethodPost,
			Field:       "to",
			Description: "Specify where to import to.",
		}, {
			Method:      http.MethodPost,
			Field:       "key",
			Description: "Specify which setting key to import.",
		}, {
			Method:      http.MethodPost,
			Field:       "validate",
			Description: "Validate only.",
		}},
		BelongsTo:  module,
		StructFunc: handleImportSingleSetting,
	}); err != nil {
		return err
	}

	return nil
}

func handleExportSingleSetting(ar *api.Request) (data []byte, err error) {
	var request *ExportRequest

	// Get parameters.
	q := ar.URL.Query()
	if len(q) > 0 {
		request = &ExportRequest{
			From: q.Get("from"),
			Key:  q.Get("key"),
		}
	} else {
		request = &ExportRequest{}
		if err := json.Unmarshal(ar.InputData, request); err != nil {
			return nil, fmt.Errorf("%w: failed to parse export request: %s", ErrExportFailed, err)
		}
	}

	// Check parameters.
	if request.From == "" || request.Key == "" {
		return nil, errors.New("missing parameters")
	}

	// Export.
	export, err := ExportSingleSetting(request.Key, request.From)
	if err != nil {
		return nil, err
	}

	// Make some yummy yaml.
	yamlData, err := yaml.Marshal(export)
	if err != nil {
		return nil, fmt.Errorf("%w: failed to marshal to yaml: %s", ErrExportFailed, err)
	}

	// TODO: Add checksum for integrity.

	return yamlData, nil
}

func handleImportSingleSetting(ar *api.Request) (any, error) {
	var request *SingleSettingImportRequest

	// Get parameters.
	q := ar.URL.Query()
	if len(q) > 0 {
		request = &SingleSettingImportRequest{
			ImportRequest: ImportRequest{
				Target:       q.Get("to"),
				ValidateOnly: q.Has("validate"),
				RawExport:    string(ar.InputData),
			},
		}
	} else {
		request = &SingleSettingImportRequest{}
		if err := json.Unmarshal(ar.InputData, request); err != nil {
			return nil, fmt.Errorf("%w: failed to parse import request: %s", ErrInvalidImport, err)
		}
	}

	// Check if we need to parse the export.
	switch {
	case request.Export != nil && request.RawExport != "":
		return nil, fmt.Errorf("%w: both Export and RawExport are defined", ErrInvalidImport)
	case request.RawExport != "":
		// TODO: Verify checksum for integrity.

		export := &SingleSettingExport{}
		if err := yaml.Unmarshal([]byte(request.RawExport), export); err != nil {
			return nil, fmt.Errorf("%w: failed to parse export: %s", ErrInvalidImport, err)
		}
		request.Export = export
	}

	// Optional check if the setting key matches.
	if q.Has("key") && q.Get("key") != request.Export.ID {
		return nil, ErrMismatch
	}

	// Import.
	return ImportSingeSetting(request)
}

// ExportSingleSetting export a single setting.
func ExportSingleSetting(key, from string) (*SingleSettingExport, error) {
	var value any
	if from == ExportTargetGlobal {
		option, err := config.GetOption(key)
		if err != nil {
			return nil, fmt.Errorf("%w: configuration %s", ErrTargetNotFound, err)
		}
		value = option.UserValue()
		if value == nil {
			return nil, ErrUnchanged
		}
	} else {
		r, err := db.Get(profile.ProfilesDBPath + from)
		if err != nil {
			return nil, fmt.Errorf("%w: failed to find profile: %s", ErrTargetNotFound, err)
		}
		p, err := profile.EnsureProfile(r)
		if err != nil {
			return nil, fmt.Errorf("%w: failed to load profile: %s", ErrExportFailed, err)
		}
		flattened := config.Flatten(p.Config)
		value = flattened[key]
		if value == nil {
			return nil, ErrUnchanged
		}
	}

	return &SingleSettingExport{
		Type:  TypeSingleSetting,
		ID:    key,
		Value: value,
	}, nil
}

// ImportSingeSetting imports a single setting.
func ImportSingeSetting(r *SingleSettingImportRequest) (*ImportResult, error) {
	// Check import.
	if r.Export.Type != TypeSingleSetting {
		return nil, ErrMismatch
	}

	// Get option and validate value.
	option, err := config.GetOption(r.Export.ID)
	if err != nil {
		return nil, fmt.Errorf("%w: configuration %s", ErrTargetNotFound, err)
	}
	if option.ValidateValue(r.Export.Value) != nil {
		return nil, fmt.Errorf("%w: configuration value is invalid: %s", ErrInvalidSetting, err)
	}

	// Import single global setting.
	if r.Target == ExportTargetGlobal {
		// Stop here if we are only validating.
		if r.ValidateOnly {
			return &ImportResult{
				RestartRequired:  option.RequiresRestart,
				ReplacesExisting: option.IsSetByUser(),
			}, nil
		}

		// Actually import the setting.
		err = config.SetConfigOption(r.Export.ID, r.Export.Value)
		if err != nil {
			return nil, fmt.Errorf("%w: configuration value is invalid: %s", ErrInvalidSetting, err)
		}
	} else {
		// Import single setting into profile.
		rec, err := db.Get(profile.ProfilesDBPath + r.Target)
		if err != nil {
			return nil, fmt.Errorf("%w: failed to find profile: %s", ErrTargetNotFound, err)
		}
		p, err := profile.EnsureProfile(rec)
		if err != nil {
			return nil, fmt.Errorf("%w: failed to load profile: %s", ErrImportFailed, err)
		}

		// Stop here if we are only validating.
		if r.ValidateOnly {
			return &ImportResult{
				RestartRequired:  option.RequiresRestart,
				ReplacesExisting: option.IsSetByUser(),
			}, nil
		}

		// Set imported setting on profile.
		flattened := config.Flatten(p.Config)
		flattened[r.Export.ID] = r.Export.Value
		p.Config = config.Expand(flattened)

		// Save profile back to db.
		err = p.Save()
		if err != nil {
			return nil, fmt.Errorf("%w: failed to save profile: %s", ErrImportFailed, err)
		}
	}

	return &ImportResult{
		RestartRequired:  option.RequiresRestart,
		ReplacesExisting: option.IsSetByUser(),
	}, nil
}
