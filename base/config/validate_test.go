package config

import (
	"testing"
)

func registerPossibleValuesOption(t *testing.T) *Option {
	t.Helper()

	options = make(map[string]*Option)
	err := Register(&Option{
		Name:           "choice name",
		Key:            "choice",
		Description:    "choice description",
		ReleaseLevel:   ReleaseLevelStable,
		ExpertiseLevel: ExpertiseLevelUser,
		OptType:        OptTypeString,
		DefaultValue:   "a",
		PossibleValues: []PossibleValue{
			{Name: "A", Value: "a"},
			{Name: "B", Value: "b"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	opt, err := GetOption("choice")
	if err != nil {
		t.Fatal(err)
	}
	return opt
}

// TestNullValueWithPossibleValues checks that a JSON null for an option with
// possible values is rejected instead of panicking.
func TestNullValueWithPossibleValues(t *testing.T) { //nolint:paralleltest // Uses global state.
	opt := registerPossibleValuesOption(t)

	// Via the settings import path.
	err := opt.ValidateValue(nil)
	if err == nil {
		t.Error("expected nil value to be rejected")
	}

	// Via a config file containing null.
	m, err := JSONToMap([]byte(`{"choice": null}`))
	if err != nil {
		t.Fatal(err)
	}
	validationErrors, _ := ReplaceConfig(m)
	if len(validationErrors) != 1 {
		t.Errorf("expected 1 validation error, got %d", len(validationErrors))
	}
}
