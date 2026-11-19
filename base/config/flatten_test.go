package config

import "testing"

func TestFlattenDuplicateKeys(t *testing.T) {
	t.Parallel()

	// Map order is random; repeat to catch order-dependent results.
	for range 50 {
		flat := Flatten(map[string]any{
			"core":         map[string]any{"devMode": true},
			"core/devMode": false,
		})
		if v, ok := flat["core/devMode"].(bool); !ok || v {
			t.Fatalf("core/devMode = %v, want false (the flattened key wins)", flat["core/devMode"])
		}
	}
}
