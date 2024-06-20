package sync

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/vincent-petithory/dataurl"

	"github.com/safing/portmaster/base/api"
	"github.com/safing/portmaster/base/config"
	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/service/profile"
	"github.com/safing/portmaster/service/profile/binmeta"
)

// ProfileExport holds an export of a profile.
type ProfileExport struct { //nolint:maligned
	Type Type `json:"type" yaml:"type"`

	// Identification
	ID     string                `json:"id,omitempty"     yaml:"id,omitempty"`
	Source profile.ProfileSource `json:"source,omitempty" yaml:"source,omitempty"`

	// Human Metadata
	Name                string `json:"name"                  yaml:"name"`
	Description         string `json:"description,omitempty" yaml:"description,omitempty"`
	Homepage            string `json:"homepage,omitempty"    yaml:"homepage,omitempty"`
	PresentationPath    string `json:"presPath,omitempty"    yaml:"presPath,omitempty"`
	UsePresentationPath bool   `json:"usePresPath,omitempty" yaml:"usePresPath,omitempty"`
	IconData            string `json:"iconData,omitempty"    yaml:"iconData,omitempty"` // DataURL

	// Process matching
	Fingerprints []ProfileFingerprint `json:"fingerprints" yaml:"fingerprints"`

	// Settings
	Config map[string]any `json:"config,omitempty" yaml:"config,omitempty"`

	// Metadata
	LastEdited *time.Time `json:"lastEdited,omitempty" yaml:"lastEdited,omitempty"`
	Created    *time.Time `json:"created,omitempty"    yaml:"created,omitempty"`
	Internal   bool       `json:"internal,omitempty"   yaml:"internal,omitempty"`
}

// ProfileIcon represents a profile icon only.
type ProfileIcon struct {
	IconData string `json:"iconData,omitempty" yaml:"iconData,omitempty"` // DataURL
}

// ProfileFingerprint represents a profile fingerprint.
type ProfileFingerprint struct {
	Type       string `json:"type"                 yaml:"type"`
	Key        string `json:"key,omitempty"        yaml:"key,omitempty"`
	Operation  string `json:"operation"            yaml:"operation"`
	Value      string `json:"value"                yaml:"value"`
	MergedFrom string `json:"mergedFrom,omitempty" yaml:"mergedFrom,omitempty"`
}

// ProfileExportRequest is a request for a profile export.
type ProfileExportRequest struct {
	ID string `json:"id"`
}

// ProfileImportRequest is a request to import Profile.
type ProfileImportRequest struct {
	ImportRequest `json:",inline"`

	// AllowUnknown allows the import of unknown settings.
	// Otherwise, attempting to import an unknown setting will result in an error.
	AllowUnknown bool `json:"allowUnknown"`

	// AllowReplace allows the import to replace other existing profiles.
	AllowReplace bool `json:"allowReplaceProfiles"`

	Export *ProfileExport `json:"export"`
}

// ProfileImportResult is returned by successful import operations.
type ProfileImportResult struct {
	ImportResult `json:",inline"`

	ReplacesProfiles []string `json:"replacesProfiles"`
}

func registerProfileAPI() error {
	if err := api.RegisterEndpoint(api.Endpoint{
		Name:        "Export App Profile",
		Description: "Exports app fingerprints, settings and metadata in a share-able format.",
		Path:        "sync/profile/export",
		Read:        api.PermitAdmin,
		Write:       api.PermitAdmin,
		Parameters: []api.Parameter{{
			Method:      http.MethodGet,
			Field:       "id",
			Description: "Specify scoped profile ID to export.",
		}},
		DataFunc: handleExportProfile,
	}); err != nil {
		return err
	}

	if err := api.RegisterEndpoint(api.Endpoint{
		Name:        "Import App Profile",
		Description: "Imports full app profiles, including fingerprints, setting and metadata from the share-able format.",
		Path:        "sync/profile/import",
		Read:        api.PermitAdmin,
		Write:       api.PermitAdmin,
		Parameters: []api.Parameter{
			{
				Method:      http.MethodPost,
				Field:       "allowReplace",
				Description: "Allow replacing existing profiles.",
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
			},
		},
		StructFunc: handleImportProfile,
	}); err != nil {
		return err
	}

	return nil
}

func handleExportProfile(ar *api.Request) (data []byte, err error) {
	var request *ProfileExportRequest

	// Get parameters.
	q := ar.URL.Query()
	if len(q) > 0 {
		request = &ProfileExportRequest{
			ID: q.Get("id"),
		}
	} else {
		request = &ProfileExportRequest{}
		if err := json.Unmarshal(ar.InputData, request); err != nil {
			return nil, fmt.Errorf("%w: failed to parse export request: %w", ErrExportFailed, err)
		}
	}

	// Check parameters.
	if request.ID == "" {
		return nil, errors.New("missing parameters")
	}

	// Export.
	export, err := ExportProfile(request.ID)
	if err != nil {
		return nil, err
	}

	return serializeProfileExport(export, ar)
}

func handleImportProfile(ar *api.Request) (any, error) {
	var request ProfileImportRequest

	// Get parameters.
	q := ar.URL.Query()
	if len(q) > 0 {
		request = ProfileImportRequest{
			ImportRequest: ImportRequest{
				ValidateOnly: q.Has("validate"),
				RawExport:    string(ar.InputData),
				RawMime:      ar.Header.Get("Content-Type"),
			},
			AllowUnknown: q.Has("allowUnknown"),
			AllowReplace: q.Has("allowReplace"),
		}
	} else {
		if err := json.Unmarshal(ar.InputData, &request); err != nil {
			return nil, fmt.Errorf("%w: failed to parse import request: %w", ErrInvalidImportRequest, err)
		}
	}

	// Check if we need to parse the export.
	switch {
	case request.Export != nil && request.RawExport != "":
		return nil, fmt.Errorf("%w: both Export and RawExport are defined", ErrInvalidImportRequest)
	case request.RawExport != "":
		// Parse export.
		export := &ProfileExport{}
		if err := parseExport(&request.ImportRequest, export); err != nil {
			return nil, err
		}
		request.Export = export
	case request.Export != nil:
		// Export is aleady parsed.
	default:
		return nil, ErrInvalidImportRequest
	}

	// Import.
	return ImportProfile(&request, profile.SourceLocal)
}

// ExportProfile exports a profile.
func ExportProfile(scopedID string) (*ProfileExport, error) {
	// Get Profile.
	r, err := db.Get(profile.ProfilesDBPath + scopedID)
	if err != nil {
		return nil, fmt.Errorf("%w: failed to find profile: %w", ErrTargetNotFound, err)
	}
	p, err := profile.EnsureProfile(r)
	if err != nil {
		return nil, fmt.Errorf("%w: failed to load profile: %w", ErrExportFailed, err)
	}

	// Copy exportable profile data.
	export := &ProfileExport{
		Type: TypeProfile,

		// Identification
		ID:     p.ID,
		Source: p.Source,

		// Human Metadata
		Name:                p.Name,
		Description:         p.Description,
		Homepage:            p.Homepage,
		PresentationPath:    p.PresentationPath,
		UsePresentationPath: p.UsePresentationPath,

		// Process matching
		Fingerprints: convertFingerprintsToExport(p.Fingerprints),

		// Settings
		Config: p.Config,

		// Metadata
		Internal: p.Internal,
	}
	// Add optional timestamps.
	if p.LastEdited > 0 {
		lastEdited := time.Unix(p.LastEdited, 0)
		export.LastEdited = &lastEdited
	}
	if p.Created > 0 {
		created := time.Unix(p.Created, 0)
		export.Created = &created
	}

	// Derive ID to ensure the ID is always correct.
	export.ID = profile.DeriveProfileID(p.Fingerprints)

	// Add first exportable icon to export.
	if len(p.Icons) > 0 {
		var err error
		for _, icon := range p.Icons {
			var iconDataURL string
			iconDataURL, err = icon.GetIconAsDataURL()
			if err == nil {
				export.IconData = iconDataURL
				break
			}
		}
		if err != nil {
			return nil, fmt.Errorf("%w: failed to export icon: %w", ErrExportFailed, err)
		}
	}

	// Remove presentation path if both Name and Icon are set.
	if export.Name != "" && export.IconData != "" {
		p.UsePresentationPath = false
	}
	if !p.UsePresentationPath {
		p.PresentationPath = ""
	}

	return export, nil
}

// ImportProfile imports a profile.
func ImportProfile(r *ProfileImportRequest, requiredProfileSource profile.ProfileSource) (*ProfileImportResult, error) {
	// Check import.
	if r.Export.Type != TypeProfile {
		return nil, ErrMismatch
	}

	// Check Source.
	if r.Export.Source != "" && r.Export.Source != requiredProfileSource {
		return nil, ErrMismatch
	}
	// Convert fingerprints to internal representation.
	fingerprints := convertFingerprintsToInternal(r.Export.Fingerprints)
	if len(fingerprints) == 0 {
		return nil, fmt.Errorf("%w: the export contains no fingerprints", ErrInvalidProfileData)
	}
	// Derive ID from fingerprints.
	profileID := profile.DeriveProfileID(fingerprints)
	if r.Export.ID != "" && r.Export.ID != profileID {
		return nil, fmt.Errorf("%w: the export profile ID does not match the fingerprints, remove to ignore", ErrInvalidProfileData)
	}
	r.Export.ID = profileID
	// Check Fingerprints.
	_, err := profile.ParseFingerprints(fingerprints, "")
	if err != nil {
		return nil, fmt.Errorf("%w: the export contains invalid fingerprints: %w", ErrInvalidProfileData, err)
	}

	// Flatten config.
	settings := config.Flatten(r.Export.Config)

	// Check settings.
	settingsResult, globalOnlySettingFound, err := checkSettings(settings)
	if err != nil {
		return nil, err
	}
	if settingsResult.ContainsUnknown && !r.AllowUnknown && !r.ValidateOnly {
		return nil, fmt.Errorf("%w: the export contains unknown settings", ErrInvalidImportRequest)
	}
	// Check if a setting is settable per app.
	if globalOnlySettingFound {
		return nil, fmt.Errorf("%w: export contains settings that cannot be set per app", ErrNotSettablePerApp)
	}

	// Create result based on settings result.
	result := &ProfileImportResult{
		ImportResult: *settingsResult,
	}

	// Check if the profile already exists.
	exists, err := db.Exists(profile.MakeProfileKey(r.Export.Source, r.Export.ID))
	if err != nil {
		return nil, fmt.Errorf("internal import error: %w", err)
	}
	if exists {
		result.ReplacesExisting = true
	}

	// Check if import will delete any profiles.
	requiredSourcePrefix := string(r.Export.Source) + "/"
	result.ReplacesProfiles = make([]string, 0, len(r.Export.Fingerprints))
	for _, fp := range r.Export.Fingerprints {
		if fp.MergedFrom != "" {
			if !strings.HasPrefix(fp.MergedFrom, requiredSourcePrefix) {
				return nil, fmt.Errorf("%w: exported profile was merged from different profile source", ErrInvalidImportRequest)
			}
			exists, err := db.Exists(profile.ProfilesDBPath + fp.MergedFrom)
			if err != nil {
				return nil, fmt.Errorf("internal import error: %w", err)
			}
			if exists {
				result.ReplacesProfiles = append(result.ReplacesProfiles, fp.MergedFrom)
			}
		}
	}

	// Stop here if we are only validating.
	if r.ValidateOnly {
		return result, nil
	}
	if result.ReplacesExisting && !r.AllowReplace {
		return nil, fmt.Errorf("%w: import would replace existing profile", ErrImportFailed)
	}

	// Create profile from export.
	// Note: Don't use profile.New(), as this will not trigger a profile refresh if active.
	in := r.Export
	p := &profile.Profile{
		// Identification
		ID:     in.ID,
		Source: requiredProfileSource,

		// Human Metadata
		Name:                in.Name,
		Description:         in.Description,
		Homepage:            in.Homepage,
		PresentationPath:    in.PresentationPath,
		UsePresentationPath: in.UsePresentationPath,

		// Process matching
		Fingerprints: fingerprints,

		// Settings
		Config: in.Config,

		// Metadata
		Internal: in.Internal,
	}
	// Add optional timestamps.
	if in.LastEdited != nil {
		p.LastEdited = in.LastEdited.Unix()
	}
	if in.Created != nil {
		p.Created = in.Created.Unix()
	}

	// Fill in required values.
	if p.Config == nil {
		p.Config = make(map[string]any)
	}
	if p.Created == 0 {
		p.Created = time.Now().Unix()
	}

	// Add icon to profile, if set.
	if in.IconData != "" {
		du, err := dataurl.DecodeString(in.IconData)
		if err != nil {
			return nil, fmt.Errorf("%w: icon data is invalid: %w", ErrImportFailed, err)
		}
		filename, err := binmeta.UpdateProfileIcon(du.Data, du.MediaType.Subtype)
		if err != nil {
			return nil, fmt.Errorf("%w: icon is invalid: %w", ErrImportFailed, err)
		}
		p.Icons = []binmeta.Icon{{
			Type:   binmeta.IconTypeAPI,
			Value:  filename,
			Source: binmeta.IconSourceImport,
		}}
	}

	// Save profile to db.
	p.SetKey(profile.MakeProfileKey(p.Source, p.ID))
	err = p.Save()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to save profile: %w", ErrImportFailed, err)
	}

	// Delete profiles that were merged into the imported profile.
	for _, profileID := range result.ReplacesProfiles {
		err := db.Delete(profile.ProfilesDBPath + profileID)
		if err != nil {
			log.Errorf("sync: failed to delete merged profile %s on import: %s", profileID, err)
		}
	}

	return result, nil
}

func convertFingerprintsToExport(fingerprints []profile.Fingerprint) []ProfileFingerprint {
	converted := make([]ProfileFingerprint, 0, len(fingerprints))
	for _, fp := range fingerprints {
		converted = append(converted, ProfileFingerprint{
			Type:       fp.Type,
			Key:        fp.Key,
			Operation:  fp.Operation,
			Value:      fp.Value,
			MergedFrom: fp.MergedFrom,
		})
	}
	return converted
}

func convertFingerprintsToInternal(fingerprints []ProfileFingerprint) []profile.Fingerprint {
	converted := make([]profile.Fingerprint, 0, len(fingerprints))
	for _, fp := range fingerprints {
		converted = append(converted, profile.Fingerprint{
			Type:       fp.Type,
			Key:        fp.Key,
			Operation:  fp.Operation,
			Value:      fp.Value,
			MergedFrom: fp.MergedFrom,
		})
	}
	return converted
}
