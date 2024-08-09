package sync

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	yaml "gopkg.in/yaml.v3"

	"github.com/safing/jess/filesig"
	"github.com/safing/portmaster/base/api"
	"github.com/safing/structures/container"
	"github.com/safing/structures/dsd"
)

// Type is the type of an export.
type Type string

// Export Types.
const (
	TypeProfile       = "profile"
	TypeSettings      = "settings"
	TypeSingleSetting = "single-setting"
)

// Export IDs.
const (
	ExportTargetGlobal = "global"
)

// Messages.
var (
	MsgNone           = ""
	MsgValid          = "Import is valid."
	MsgSuccess        = "Import successful."
	MsgRequireRestart = "Import successful. Restart required for setting to take effect."
)

// ExportRequest is a request for an export.
type ExportRequest struct {
	From string   `json:"from"`
	Keys []string `json:"keys"`
}

// ImportRequest is a request to import an export.
type ImportRequest struct {
	// Where the export should be import to.
	Target string `json:"target"`
	// Only validate, but do not actually change anything.
	ValidateOnly bool `json:"validateOnly"`

	RawExport string `json:"rawExport"`
	RawMime   string `json:"rawMime"`
}

// ImportResult is returned by successful import operations.
type ImportResult struct {
	RestartRequired  bool `json:"restartRequired"`
	ReplacesExisting bool `json:"replacesExisting"`
	ContainsUnknown  bool `json:"containsUnknown"`
}

// Errors.
var (
	ErrMismatch = api.ErrorWithStatus(
		errors.New("the supplied export cannot be imported here"),
		http.StatusPreconditionFailed,
	)
	ErrSettingNotFound = api.ErrorWithStatus(
		errors.New("setting not found"),
		http.StatusPreconditionFailed,
	)
	ErrTargetNotFound = api.ErrorWithStatus(
		errors.New("import/export target does not exist"),
		http.StatusGone,
	)
	ErrUnchanged = api.ErrorWithStatus(
		errors.New("cannot export unchanged setting"),
		http.StatusGone,
	)
	ErrNotSettablePerApp = api.ErrorWithStatus(
		errors.New("cannot be set per app"),
		http.StatusGone,
	)
	ErrInvalidImportRequest = api.ErrorWithStatus(
		errors.New("invalid import request"),
		http.StatusUnprocessableEntity,
	)
	ErrInvalidSettingValue = api.ErrorWithStatus(
		errors.New("invalid setting value"),
		http.StatusUnprocessableEntity,
	)
	ErrInvalidProfileData = api.ErrorWithStatus(
		errors.New("invalid profile data"),
		http.StatusUnprocessableEntity,
	)
	ErrImportFailed = api.ErrorWithStatus(
		errors.New("import failed"),
		http.StatusInternalServerError,
	)
	ErrExportFailed = api.ErrorWithStatus(
		errors.New("export failed"),
		http.StatusInternalServerError,
	)
)

func serializeExport(export any, ar *api.Request) (data []byte, err error) {
	// Get format.
	format := dsd.FormatFromAccept(ar.Header.Get("Accept"))

	// Serialize and add checksum.
	switch format {
	case dsd.JSON:
		data, err = json.Marshal(export)
		if err == nil {
			data, err = filesig.AddJSONChecksum(data)
		}
	case dsd.YAML:
		data, err = yaml.Marshal(export)
		if err == nil {
			data, err = filesig.AddYAMLChecksum(data, filesig.TextPlacementBottom)
		}
	default:
		return nil, dsd.ErrIncompatibleFormat
	}
	if err != nil {
		return nil, fmt.Errorf("failed to serialize: %w", err)
	}

	// Set Content-Type HTTP Header.
	ar.ResponseHeader.Set("Content-Type", dsd.FormatToMimeType[format])

	return data, nil
}

func serializeProfileExport(export *ProfileExport, ar *api.Request) ([]byte, error) {
	// Do a regular serialize, if we don't need parts.
	switch {
	case export.IconData == "":
		// With no icon, do a regular export.
		return serializeExport(export, ar)
	case dsd.FormatFromAccept(ar.Header.Get("Accept")) != dsd.YAML:
		// Only export in parts for yaml.
		return serializeExport(export, ar)
	}

	// Step 1: Separate profile icon.
	profileIconExport := &ProfileIcon{
		IconData: export.IconData,
	}
	export.IconData = ""

	// Step 2: Serialize main export.
	profileData, err := yaml.Marshal(export)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize profile data: %w", err)
	}

	// Step 3: Serialize icon only.
	iconData, err := yaml.Marshal(profileIconExport)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize profile icon: %w", err)
	}

	// Step 4: Stitch data together and add copyright notice for icon.
	exportData := container.New(
		profileData,
		[]byte(`
# The application icon below is the property of its respective owner.
# The icon is used for identification purposes only, and does not imply any endorsement or affiliation with their respective owners.
# It is the sole responsibility of the individual or entity sharing this dataset to ensure they have the necessary permissions to do so.
`),
		iconData,
	).CompileData()

	// Step 4: Add checksum.
	exportData, err = filesig.AddYAMLChecksum(exportData, filesig.TextPlacementBottom)
	if err != nil {
		return nil, fmt.Errorf("failed to add checksum: %w", err)
	}

	// Set Content-Type HTTP Header.
	ar.ResponseHeader.Set("Content-Type", dsd.FormatToMimeType[dsd.YAML])

	return exportData, nil
}

func parseExport(request *ImportRequest, export any) error {
	format, err := dsd.MimeLoad([]byte(request.RawExport), request.RawMime, export)
	if err != nil {
		return fmt.Errorf("%w: failed to parse export: %w", ErrInvalidImportRequest, err)
	}

	// Verify checksum, if available.
	switch format {
	case dsd.JSON:
		err = filesig.VerifyJSONChecksum([]byte(request.RawExport))
	case dsd.YAML:
		err = filesig.VerifyYAMLChecksum([]byte(request.RawExport))
	default:
		// Checksums not supported.
	}
	if err != nil && !errors.Is(err, filesig.ErrChecksumMissing) {
		return fmt.Errorf("failed to verify checksum: %w", err)
	}

	return nil
}
