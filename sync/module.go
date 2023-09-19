package sync

import (
	"errors"
	"net/http"

	"github.com/safing/portbase/api"
	"github.com/safing/portbase/database"
	"github.com/safing/portbase/modules"
)

var (
	module *modules.Module

	db = database.NewInterface(&database.Options{
		Local:    true,
		Internal: true,
	})
)

func init() {
	module = modules.Register("sync", prep, nil, nil, "profiles")
}

func prep() error {
	if err := registerSettingsAPI(); err != nil {
		return err
	}
	if err := registerSingleSettingAPI(); err != nil {
		return err
	}
	return nil
}

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
	From string `json:"from"`
	Key  string `json:"key"`
}

// ImportRequest is a request to import an export.
type ImportRequest struct {
	// Where the export should be import to.
	Target string `json:"target"`
	// Only validate, but do not actually change anything.
	ValidateOnly bool `json:"validate_only"`

	RawExport string `json:"raw_export"`
}

// ImportResult is returned by successful import operations.
type ImportResult struct {
	RestartRequired  bool `json:"restart_required"`
	ReplacesExisting bool `json:"replaces_existing"`
}

// Errors.
var (
	ErrMismatch = api.ErrorWithStatus(
		errors.New("the supplied export cannot be imported here"),
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
	ErrInvalidImport = api.ErrorWithStatus(
		errors.New("invalid import"),
		http.StatusUnprocessableEntity,
	)
	ErrInvalidSetting = api.ErrorWithStatus(
		errors.New("invalid setting"),
		http.StatusUnprocessableEntity,
	)
	ErrInvalidProfile = api.ErrorWithStatus(
		errors.New("invalid profile"),
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
