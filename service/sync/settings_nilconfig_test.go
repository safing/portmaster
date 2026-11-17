package sync

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/safing/portmaster/base/config"
	"github.com/safing/portmaster/service/profile"
)

const nilConfigTestOptionKey = "test/sync/nilconfig"

// saveNilConfigProfile stores a profile with a nil Config map, as is the case
// for a stored record with `"config": null`.
func saveNilConfigProfile(t *testing.T, id string) {
	t.Helper()

	p := &profile.Profile{
		ID:     id,
		Source: profile.SourceLocal,
		Name:   "nil config",
		Config: nil,
	}
	p.SetKey(profile.MakeProfileKey(p.Source, p.ID))
	require.NoError(t, p.Save())
}

func registerNilConfigTestOption(t *testing.T) {
	t.Helper()

	_, err := config.GetOption(nilConfigTestOptionKey)
	if err == nil {
		return
	}
	require.NoError(t, config.Register(&config.Option{
		Name:         "Nil Config Test",
		Key:          nilConfigTestOptionKey,
		Description:  "test option",
		OptType:      config.OptTypeBool,
		DefaultValue: false,
		Annotations: config.Annotations{
			config.SettablePerAppAnnotation: true,
		},
	}))
}

func loadNilConfigProfile(t *testing.T, id string) *profile.Profile {
	t.Helper()

	r, err := db.Get(profile.MakeProfileKey(profile.SourceLocal, id))
	require.NoError(t, err)
	p, err := profile.EnsureProfile(r)
	require.NoError(t, err)
	return p
}

// TestImportSettingsIntoNilConfigProfile checks that importing settings into a
// profile with a nil Config map does not panic.
//
//nolint:paralleltest // Uses the global database.
func TestImportSettingsIntoNilConfigProfile(t *testing.T) {
	registerNilConfigTestOption(t)
	saveNilConfigProfile(t, "nilconfig-settings")

	req := &SettingsImportRequest{
		ImportRequest: ImportRequest{Target: "local/nilconfig-settings"},
		Export: &SettingsExport{
			Type:   TypeSettings,
			Config: map[string]any{nilConfigTestOptionKey: true},
		},
	}
	require.NotPanics(t, func() {
		_, err := ImportSettings(req)
		require.NoError(t, err)
	})

	p := loadNilConfigProfile(t, "nilconfig-settings")
	assert.Equal(t, true, config.Flatten(p.Config)[nilConfigTestOptionKey])
}

// TestImportSingleSettingIntoNilConfigProfile checks that importing a single
// setting into a profile with a nil Config map does not panic.
//
//nolint:paralleltest // Uses the global database.
func TestImportSingleSettingIntoNilConfigProfile(t *testing.T) {
	registerNilConfigTestOption(t)
	saveNilConfigProfile(t, "nilconfig-single")

	req := &SingleSettingImportRequest{
		ImportRequest: ImportRequest{Target: "local/nilconfig-single"},
		Export: &SingleSettingExport{
			Type:  TypeSingleSetting,
			ID:    nilConfigTestOptionKey,
			Value: true,
		},
	}
	require.NotPanics(t, func() {
		_, err := ImportSingeSetting(req)
		require.NoError(t, err)
	})

	p := loadNilConfigProfile(t, "nilconfig-single")
	assert.Equal(t, true, config.Flatten(p.Config)[nilConfigTestOptionKey])
}
