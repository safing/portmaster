package config

import (
	"math"
	"testing"
)

// TestIntOutOfRange checks that numbers outside of the int64 range are
// rejected instead of being silently converted to a wrong value.
func TestIntOutOfRange(t *testing.T) { //nolint:paralleltest // Uses global state.
	options = make(map[string]*Option)
	quickRegister(t, "number", OptTypeInt, 1)
	getNumber := GetAsInt("number", 1)

	for _, jsonValue := range []string{"1e19", "-1e19", "9223372036854775807", "1e300"} {
		m, err := JSONToMap([]byte(`{"number": ` + jsonValue + `}`))
		if err != nil {
			t.Fatal(err)
		}
		validationErrors, _ := ReplaceConfig(m)
		if len(validationErrors) != 1 {
			t.Errorf("%s: expected 1 validation error, got %d (value now %d)", jsonValue, len(validationErrors), getNumber())
		}
	}

	// Values in range still work.
	m, err := JSONToMap([]byte(`{"number": -9223372036854775808}`))
	if err != nil {
		t.Fatal(err)
	}
	validationErrors, _ := ReplaceConfig(m)
	if len(validationErrors) != 0 || getNumber() != math.MinInt64 {
		t.Errorf("expected min int64 to be accepted, got %d errors and value %d", len(validationErrors), getNumber())
	}

	err = setConfigOption("number", uint(math.MaxUint64), false)
	if err == nil {
		t.Errorf("expected max uint to be rejected, got value %d", getNumber())
	}
}

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
