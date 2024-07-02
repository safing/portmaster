package config

import (
	"bytes"
	"encoding/json"
	"testing"
)

var (
	jsonData = `{
  "a": "b",
  "c": {
    "d": "e",
    "f": "g",
    "h": {
      "i": "j",
      "k": "l",
      "m": {
        "n": "o"
      }
    }
  },
  "p": "q"
}`
	jsonBytes = []byte(jsonData)

	mapData = map[string]interface{}{
		"a":       "b",
		"p":       "q",
		"c/d":     "e",
		"c/f":     "g",
		"c/h/i":   "j",
		"c/h/k":   "l",
		"c/h/m/n": "o",
	}
)

func TestJSONMapConversion(t *testing.T) {
	t.Parallel()

	// convert to json
	j, err := MapToJSON(mapData)
	if err != nil {
		t.Fatal(err)
	}

	// check if to json matches
	if !bytes.Equal(jsonBytes, j) {
		t.Errorf("json does not match, got %s", j)
	}

	// convert to map
	m, err := JSONToMap(jsonBytes)
	if err != nil {
		t.Fatal(err)
	}

	// and back
	j2, err := MapToJSON(m)
	if err != nil {
		t.Fatal(err)
	}

	// check if double convert matches
	if !bytes.Equal(jsonBytes, j2) {
		t.Errorf("json does not match, got %s", j)
	}
}

func TestConfigCleaning(t *testing.T) {
	t.Parallel()

	// load
	configFlat, err := JSONToMap(jsonBytes)
	if err != nil {
		t.Fatal(err)
	}

	// clean everything
	CleanFlattenedConfig(configFlat)
	if len(configFlat) != 0 {
		t.Errorf("should be empty: %+v", configFlat)
	}

	// load manuall for hierarchical config
	configHier := make(map[string]interface{})
	err = json.Unmarshal(jsonBytes, &configHier)
	if err != nil {
		t.Fatal(err)
	}

	// clean everything
	CleanHierarchicalConfig(configHier)
	if len(configHier) != 0 {
		t.Errorf("should be empty: %+v", configHier)
	}
}
