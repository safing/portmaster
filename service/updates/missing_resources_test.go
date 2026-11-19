package updates

import (
	"errors"
	"testing"
)

var errTestBroken = errors.New("broken")

func TestMissingResources(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		updater *Updater
		want    bool
	}{
		{"no index", &Updater{}, true},
		{"empty index", &Updater{index: &Index{}}, true},
		{"corrupted", &Updater{
			index:                 &Index{Artifacts: []*Artifact{{Filename: "a"}}},
			corruptedInstallation: errTestBroken,
		}, true},
		{"installed", &Updater{index: &Index{Artifacts: []*Artifact{{Filename: "a"}}}}, false},
	}
	for _, tc := range tests {
		if got := tc.updater.missingResources(); got != tc.want {
			t.Errorf("%s: missingResources() = %v, want %v", tc.name, got, tc.want)
		}
	}
}
