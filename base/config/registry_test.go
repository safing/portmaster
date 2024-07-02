package config

import (
	"testing"
)

func TestRegistry(t *testing.T) { //nolint:paralleltest
	// reset
	options = make(map[string]*Option)

	if err := Register(&Option{
		Name:            "name",
		Key:             "key",
		Description:     "description",
		ReleaseLevel:    ReleaseLevelStable,
		ExpertiseLevel:  ExpertiseLevelUser,
		OptType:         OptTypeString,
		DefaultValue:    "water",
		ValidationRegex: "^(banana|water)$",
	}); err != nil {
		t.Error(err)
	}

	if err := Register(&Option{
		Name:            "name",
		Key:             "key",
		Description:     "description",
		ReleaseLevel:    ReleaseLevelStable,
		ExpertiseLevel:  ExpertiseLevelUser,
		OptType:         0,
		DefaultValue:    "default",
		ValidationRegex: "^[A-Z][a-z]+$",
	}); err == nil {
		t.Error("should fail")
	}

	if err := Register(&Option{
		Name:            "name",
		Key:             "key",
		Description:     "description",
		ReleaseLevel:    ReleaseLevelStable,
		ExpertiseLevel:  ExpertiseLevelUser,
		OptType:         OptTypeString,
		DefaultValue:    "default",
		ValidationRegex: "[",
	}); err == nil {
		t.Error("should fail")
	}
}
