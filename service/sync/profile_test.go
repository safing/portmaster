package sync

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/safing/portmaster/base/database"
	_ "github.com/safing/portmaster/base/database/storage/hashmap"
	"github.com/safing/portmaster/service/profile"
)

func TestMain(m *testing.M) {
	// Initialize the global database system with an in-memory "core" database.
	dir, err := os.MkdirTemp("", "portmaster-sync-test-")
	if err != nil {
		panic(err)
	}
	err = database.Initialize(dir)
	if err != nil {
		panic(err)
	}
	_, err = database.Register(&database.Database{
		Name:        "core",
		Description: "test",
		StorageType: "hashmap",
	})
	if err != nil {
		panic(err)
	}

	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

// TestImportProfileWithoutSourceDetectsExisting checks that importing a
// profile export that does not specify a source checks for an existing
// profile (and merged profiles) in the source it is actually imported to.
//
//nolint:paralleltest // Uses the global database.
func TestImportProfileWithoutSourceDetectsExisting(t *testing.T) {
	fingerprints := []profile.Fingerprint{{
		Type:      profile.FingerprintTypePathID,
		Operation: profile.FingerprintOperationEqualsID,
		Value:     "/usr/bin/test-app",
	}}
	id := profile.DeriveProfileID(fingerprints)

	// Create existing local profiles.
	existing := &profile.Profile{
		ID:           id,
		Source:       profile.SourceLocal,
		Name:         "existing",
		Fingerprints: fingerprints,
		Config:       map[string]any{},
	}
	existing.SetKey(profile.MakeProfileKey(existing.Source, existing.ID))
	require.NoError(t, existing.Save())
	merged := &profile.Profile{
		ID:     "merged",
		Source: profile.SourceLocal,
		Name:   "merged",
		Config: map[string]any{},
	}
	merged.SetKey(profile.MakeProfileKey(merged.Source, merged.ID))
	require.NoError(t, merged.Save())

	newRequest := func(validateOnly bool) *ProfileImportRequest {
		return &ProfileImportRequest{
			ImportRequest: ImportRequest{ValidateOnly: validateOnly},
			Export: &ProfileExport{
				Type: TypeProfile,
				Name: "imported",
				Fingerprints: []ProfileFingerprint{{
					Type:       profile.FingerprintTypePathID,
					Operation:  profile.FingerprintOperationEqualsID,
					Value:      "/usr/bin/test-app",
					MergedFrom: "local/merged",
				}},
			},
		}
	}

	// Validation must report that the existing profiles are replaced.
	result, err := ImportProfile(newRequest(true), profile.SourceLocal)
	require.NoError(t, err)
	assert.True(t, result.ReplacesExisting, "validation must report that the existing profile is replaced")
	assert.Equal(t, []string{"local/merged"}, result.ReplacesProfiles)

	// Import must not replace the existing profile without AllowReplace.
	_, err = ImportProfile(newRequest(false), profile.SourceLocal)
	require.ErrorIs(t, err, ErrImportFailed)
	r, err := db.Get(profile.MakeProfileKey(profile.SourceLocal, id))
	require.NoError(t, err)
	p, err := profile.EnsureProfile(r)
	require.NoError(t, err)
	assert.Equal(t, "existing", p.Name, "existing profile must not be replaced")
}

// TestExportProfileOmitsUnusedPresentationPath checks that the export does not
// contain the presentation path if it is not used, and that exporting does not
// modify the stored profile.
//
//nolint:paralleltest // Uses the global database.
func TestExportProfileOmitsUnusedPresentationPath(t *testing.T) {
	fingerprints := []profile.Fingerprint{{
		Type:      profile.FingerprintTypePathID,
		Operation: profile.FingerprintOperationEqualsID,
		Value:     "/usr/bin/export-app",
	}}
	p := &profile.Profile{
		ID:                  profile.DeriveProfileID(fingerprints),
		Source:              profile.SourceLocal,
		Name:                "export-app",
		PresentationPath:    "/home/someone/private/export-app",
		UsePresentationPath: false,
		Fingerprints:        fingerprints,
		Config:              map[string]any{},
	}
	p.SetKey(profile.MakeProfileKey(p.Source, p.ID))
	require.NoError(t, p.Save())

	export, err := ExportProfile(p.ScopedID())
	require.NoError(t, err)
	assert.Empty(t, export.PresentationPath, "unused presentation path must not be exported")
	assert.False(t, export.UsePresentationPath)

	// The stored profile must be unchanged.
	r, err := db.Get(profile.MakeProfileKey(p.Source, p.ID))
	require.NoError(t, err)
	stored, err := profile.EnsureProfile(r)
	require.NoError(t, err)
	assert.Equal(t, "/home/someone/private/export-app", stored.PresentationPath, "export must not modify the profile")
}
