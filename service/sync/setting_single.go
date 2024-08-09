package sync

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/safing/portmaster/base/api"
	"github.com/safing/portmaster/base/config"
	"github.com/safing/portmaster/service/profile"
	"github.com/safing/structures/dsd"
)

// SingleSettingExport holds an export of a single setting.
type SingleSettingExport struct {
	Type Type   `json:"type" yaml:"type"` // Must be TypeSingleSetting
	ID   string `json:"id"   yaml:"id"`   // Settings Key

	Value any `json:"value" yaml:"value"`
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
		DataFunc: handleExportSingleSetting,
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
			Keys: q["key"], // Get []string by direct map access.
		}
	} else {
		request = &ExportRequest{}
		if err := json.Unmarshal(ar.InputData, request); err != nil {
			return nil, fmt.Errorf("%w: failed to parse export request: %w", ErrExportFailed, err)
		}
	}

	// Check parameters.
	if request.From == "" || len(request.Keys) != 1 {
		return nil, errors.New("missing or malformed parameters")
	}

	// Export.
	export, err := ExportSingleSetting(request.Keys[0], request.From)
	if err != nil {
		return nil, err
	}

	return serializeExport(export, ar)
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
				RawMime:      ar.Header.Get("Content-Type"),
			},
		}
	} else {
		request = &SingleSettingImportRequest{}
		if _, err := dsd.MimeLoad(ar.InputData, ar.Header.Get("Accept"), request); err != nil {
			return nil, fmt.Errorf("%w: failed to parse import request: %w", ErrInvalidImportRequest, err)
		}
	}

	// Check if we need to parse the export.
	switch {
	case request.Export != nil && request.RawExport != "":
		return nil, fmt.Errorf("%w: both Export and RawExport are defined", ErrInvalidImportRequest)
	case request.RawExport != "":
		// Parse export.
		export := &SingleSettingExport{}
		if err := parseExport(&request.ImportRequest, export); err != nil {
			return nil, err
		}
		request.Export = export
	case request.Export != nil:
		// Export is aleady parsed.
	default:
		return nil, ErrInvalidImportRequest
	}

	// Optional check if the setting key matches.
	if len(q) > 0 && q.Has("key") && q.Get("key") != request.Export.ID {
		return nil, ErrMismatch
	}

	// Import.
	return ImportSingeSetting(request)
}

// ExportSingleSetting export a single setting.
func ExportSingleSetting(key, from string) (*SingleSettingExport, error) {
	option, err := config.GetOption(key)
	if err != nil {
		return nil, fmt.Errorf("%w: configuration %w", ErrSettingNotFound, err)
	}

	var value any
	if from == ExportTargetGlobal {
		value = option.UserValue()
		if value == nil {
			return nil, ErrUnchanged
		}
	} else {
		// Check if the setting is settable per app.
		if !option.AnnotationEquals(config.SettablePerAppAnnotation, true) {
			return nil, ErrNotSettablePerApp
		}
		// Get and load profile.
		r, err := db.Get(profile.ProfilesDBPath + from)
		if err != nil {
			return nil, fmt.Errorf("%w: failed to find profile: %w", ErrTargetNotFound, err)
		}
		p, err := profile.EnsureProfile(r)
		if err != nil {
			return nil, fmt.Errorf("%w: failed to load profile: %w", ErrExportFailed, err)
		}
		// Flatten config and get key we are looking for.
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
		return nil, fmt.Errorf("%w: configuration %w", ErrSettingNotFound, err)
	}
	if err := option.ValidateValue(r.Export.Value); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidSettingValue, err)
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
		if err := config.SetConfigOption(r.Export.ID, r.Export.Value); err != nil {
			return nil, fmt.Errorf("%w: %w", ErrInvalidSettingValue, err)
		}
	} else {
		// Check if the setting is settable per app.
		if !option.AnnotationEquals(config.SettablePerAppAnnotation, true) {
			return nil, ErrNotSettablePerApp
		}
		// Import single setting into profile.
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
			return &ImportResult{
				RestartRequired:  option.RequiresRestart,
				ReplacesExisting: option.IsSetByUser(),
			}, nil
		}

		// Set imported setting on profile.
		config.PutValueIntoHierarchicalConfig(p.Config, r.Export.ID, r.Export.Value)

		// Mark profile as edited by user.
		p.LastEdited = time.Now().Unix()

		// Save profile back to db.
		if err := p.Save(); err != nil {
			return nil, fmt.Errorf("%w: failed to save profile: %w", ErrImportFailed, err)
		}
	}

	return &ImportResult{
		RestartRequired:  option.RequiresRestart,
		ReplacesExisting: option.IsSetByUser(),
	}, nil
}
