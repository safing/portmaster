package config

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLoadConfigIgnoresUnknownKeys checks that config files written by older
// versions, which still contain settings of removed features (e.g. the SPN
// settings "spn/..."), load without errors and that the unknown keys are
// dropped from the file on the next save.
func TestLoadConfigIgnoresUnknownKeys(t *testing.T) { //nolint:paralleltest // Uses global state.
	options = make(map[string]*Option)
	quickRegister(t, "test/known", OptTypeString, "default")

	oldPath := configFilePath
	configFilePath = filepath.Join(t.TempDir(), "config.json")
	defer func() { configFilePath = oldPath }()

	err := os.WriteFile(configFilePath, []byte(`{
  "spn": {
    "enable": true,
    "use": true,
    "routingAlgorithm": "triple-hop",
    "exitHubPolicy": ["- DE"]
  },
  "test": {
    "known": "set"
  }
}`), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	// Loading with requireValidConfig must not fail because of unknown keys.
	err = loadConfig(true)
	if err != nil {
		t.Fatalf("loading config with unknown keys failed: %s", err)
	}
	if errs := GetLoadedConfigValidationErrors(); len(errs) != 0 {
		t.Fatalf("unexpected validation errors: %v", errs)
	}
	if v := GetAsString("test/known", "")(); v != "set" {
		t.Fatalf("known key was not loaded: got %q", v)
	}

	// Saving writes only registered options, so the unknown keys are dropped.
	err = SaveConfig()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(configFilePath)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := JSONToMap(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(saved) != 1 || saved["test/known"] != "set" {
		t.Fatalf("unexpected saved config: %v", saved)
	}

	// Per-app settings (perspectives) ignore unknown keys as well.
	perspective, err := NewPerspective(map[string]any{
		"spn":  map[string]any{"use": false},
		"test": map[string]any{"known": "app"},
	})
	if err != nil {
		t.Fatalf("perspective with unknown keys failed: %s", err)
	}
	if v, ok := perspective.GetAsString("test/known"); !ok || v != "app" {
		t.Fatalf("known key missing from perspective: %q, %v", v, ok)
	}
}
