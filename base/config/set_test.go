//nolint:goconst
package config

import "testing"

func TestLayersGetters(t *testing.T) { //nolint:paralleltest
	// reset
	options = make(map[string]*Option)

	mapData, err := JSONToMap([]byte(`
		{
			"monkey": "1",
			"elephant": 2,
			"zebras": {
				"zebra": ["black", "white"],
				"weird_zebra": ["black", -1]
			},
			"env": {
				"hot": true
			}
		}
    `))
	if err != nil {
		t.Fatal(err)
	}

	validationErrors, _ := ReplaceConfig(mapData)
	if len(validationErrors) > 0 {
		t.Fatalf("%d errors, first: %s", len(validationErrors), validationErrors[0].Error())
	}

	// Test missing values

	missingString := GetAsString("missing", "fallback")
	if missingString() != "fallback" {
		t.Error("expected fallback value: fallback")
	}

	missingStringArray := GetAsStringArray("missing", []string{"fallback"})
	if len(missingStringArray()) != 1 || missingStringArray()[0] != "fallback" {
		t.Error("expected fallback value: [fallback]")
	}

	missingInt := GetAsInt("missing", -1)
	if missingInt() != -1 {
		t.Error("expected fallback value: -1")
	}

	missingBool := GetAsBool("missing", false)
	if missingBool() {
		t.Error("expected fallback value: false")
	}

	// Test value mismatch

	notString := GetAsString("elephant", "fallback")
	if notString() != "fallback" {
		t.Error("expected fallback value: fallback")
	}

	notStringArray := GetAsStringArray("elephant", []string{"fallback"})
	if len(notStringArray()) != 1 || notStringArray()[0] != "fallback" {
		t.Error("expected fallback value: [fallback]")
	}

	mixedStringArray := GetAsStringArray("zebras/weird_zebra", []string{"fallback"})
	if len(mixedStringArray()) != 1 || mixedStringArray()[0] != "fallback" {
		t.Error("expected fallback value: [fallback]")
	}

	notInt := GetAsInt("monkey", -1)
	if notInt() != -1 {
		t.Error("expected fallback value: -1")
	}

	notBool := GetAsBool("monkey", false)
	if notBool() {
		t.Error("expected fallback value: false")
	}
}

func TestLayersSetters(t *testing.T) { //nolint:paralleltest
	// reset
	options = make(map[string]*Option)

	_ = Register(&Option{
		Name:            "name",
		Key:             "monkey",
		Description:     "description",
		ReleaseLevel:    ReleaseLevelStable,
		ExpertiseLevel:  ExpertiseLevelUser,
		OptType:         OptTypeString,
		DefaultValue:    "banana",
		ValidationRegex: "^(banana|water)$",
	})
	_ = Register(&Option{
		Name:            "name",
		Key:             "zebras/zebra",
		Description:     "description",
		ReleaseLevel:    ReleaseLevelStable,
		ExpertiseLevel:  ExpertiseLevelUser,
		OptType:         OptTypeStringArray,
		DefaultValue:    []string{"black", "white"},
		ValidationRegex: "^[a-z]+$",
	})
	_ = Register(&Option{
		Name:            "name",
		Key:             "elephant",
		Description:     "description",
		ReleaseLevel:    ReleaseLevelStable,
		ExpertiseLevel:  ExpertiseLevelUser,
		OptType:         OptTypeInt,
		DefaultValue:    2,
		ValidationRegex: "",
	})
	_ = Register(&Option{
		Name:            "name",
		Key:             "hot",
		Description:     "description",
		ReleaseLevel:    ReleaseLevelStable,
		ExpertiseLevel:  ExpertiseLevelUser,
		OptType:         OptTypeBool,
		DefaultValue:    true,
		ValidationRegex: "",
	})

	// correct types
	if err := SetConfigOption("monkey", "banana"); err != nil {
		t.Error(err)
	}
	if err := SetConfigOption("zebras/zebra", []string{"black", "white"}); err != nil {
		t.Error(err)
	}
	if err := SetDefaultConfigOption("elephant", 2); err != nil {
		t.Error(err)
	}
	if err := SetDefaultConfigOption("hot", true); err != nil {
		t.Error(err)
	}

	// incorrect types
	if err := SetConfigOption("monkey", []string{"black", "white"}); err == nil {
		t.Error("should fail")
	}
	if err := SetConfigOption("zebras/zebra", 2); err == nil {
		t.Error("should fail")
	}
	if err := SetDefaultConfigOption("elephant", true); err == nil {
		t.Error("should fail")
	}
	if err := SetDefaultConfigOption("hot", "banana"); err == nil {
		t.Error("should fail")
	}
	if err := SetDefaultConfigOption("hot", []byte{0}); err == nil {
		t.Error("should fail")
	}

	// validation fail
	if err := SetConfigOption("monkey", "dirt"); err == nil {
		t.Error("should fail")
	}
	if err := SetConfigOption("zebras/zebra", []string{"Element649"}); err == nil {
		t.Error("should fail")
	}

	// unregistered checking
	if err := SetConfigOption("invalid", "banana"); err == nil {
		t.Error("should fail")
	}
	if err := SetConfigOption("invalid", []string{"black", "white"}); err == nil {
		t.Error("should fail")
	}
	if err := SetConfigOption("invalid", 2); err == nil {
		t.Error("should fail")
	}
	if err := SetConfigOption("invalid", true); err == nil {
		t.Error("should fail")
	}
	if err := SetConfigOption("invalid", []byte{0}); err == nil {
		t.Error("should fail")
	}

	// delete
	if err := SetConfigOption("monkey", nil); err != nil {
		t.Error(err)
	}
	if err := SetDefaultConfigOption("elephant", nil); err != nil {
		t.Error(err)
	}
	if err := SetDefaultConfigOption("invalid_delete", nil); err == nil {
		t.Error("should fail")
	}
}
