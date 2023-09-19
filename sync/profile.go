package sync

import (
	"time"

	"github.com/safing/portmaster/profile"
)

// ProfileExport holds an export of a profile.
type ProfileExport struct { //nolint:maligned
	Type Type

	// Identification (sync or import as new only)
	ID     string
	Source string

	// Human Metadata
	Name                string
	Description         string
	Homepage            string
	Icons               []profile.Icon
	PresentationPath    string
	UsePresentationPath bool

	// Process matching
	Fingerprints []profile.Fingerprint

	// Settings
	Config map[string]any

	// Metadata (sync only)
	LastEdited time.Time
	Created    time.Time
	Internal   bool
}

// ProfileImportRequest is a request to import Profile.
type ProfileImportRequest struct {
	ImportRequest

	// Reset all settings and fingerprints of target before import.
	Reset bool

	Export *ProfileExport
}
