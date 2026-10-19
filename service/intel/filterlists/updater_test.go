package filterlists

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/hashicorp/go-version"

	"github.com/safing/portmaster/service/updates"
)

func TestResolveUpdateOrder(t *testing.T) {
	t.Parallel()

	const (
		vZero     = "0.0.0"
		v20260925 = "20260925.0.0"
		v20260927 = "20260927.0.0"
		v20260930 = "20260930.21.5"
		v20261001 = "20261001.0.0"
		v20261004 = "20261004.0.0"
		v20261005 = "20261005.0.0"
	)

	tests := []struct {
		name           string
		files          map[string]string // filename -> version
		cacheDBVersion string
		expected       []string
	}{
		{
			name: "fresh install, base newer than diffs",
			files: map[string]string{
				baseListFilePath:         v20261001,
				intermediateListFilePath: v20260927,
				urgentListFilePath:       v20260930,
			},
			cacheDBVersion: vZero,
			expected:       []string{baseListFilePath},
		},
		{
			name: "fresh install, diffs newer than base",
			files: map[string]string{
				baseListFilePath:         v20261001,
				intermediateListFilePath: v20261004,
				urgentListFilePath:       v20261005,
			},
			cacheDBVersion: vZero,
			expected:       []string{baseListFilePath, intermediateListFilePath, urgentListFilePath},
		},
		{
			name: "only urgent is newer",
			files: map[string]string{
				baseListFilePath:         v20261001,
				intermediateListFilePath: v20261004,
				urgentListFilePath:       v20261005,
			},
			cacheDBVersion: v20261004,
			expected:       []string{urgentListFilePath},
		},
		{
			name: "diffs newer than base already applied",
			files: map[string]string{
				baseListFilePath:         v20261001,
				intermediateListFilePath: v20261004,
				urgentListFilePath:       v20261005,
			},
			cacheDBVersion: v20261001,
			expected:       []string{intermediateListFilePath, urgentListFilePath},
		},
		{
			name: "new base starts the update",
			files: map[string]string{
				baseListFilePath:         v20261001,
				intermediateListFilePath: v20261004,
			},
			cacheDBVersion: v20260925,
			expected:       []string{baseListFilePath, intermediateListFilePath},
		},
		{
			name: "new base skips older diffs",
			files: map[string]string{
				baseListFilePath:         v20261001,
				intermediateListFilePath: v20260927,
				urgentListFilePath:       v20260930,
			},
			cacheDBVersion: v20260925,
			expected:       []string{baseListFilePath},
		},
		{
			name: "everything already applied",
			files: map[string]string{
				baseListFilePath:         v20261001,
				intermediateListFilePath: v20261004,
				urgentListFilePath:       v20261005,
			},
			cacheDBVersion: v20261005,
			expected:       nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cacheDBVersion, err := version.NewSemver(tc.cacheDBVersion)
			if err != nil {
				t.Fatalf("invalid cache database version %q: %s", tc.cacheDBVersion, err)
			}

			result, err := resolveUpdateOrder(makeTestArtifacts(t, tc.files), cacheDBVersion)
			if err != nil {
				t.Fatalf("unexpected error: %s", err)
			}

			resultNames := make([]string, 0, len(result))
			for _, file := range result {
				resultNames = append(resultNames, file.Filename)
			}
			if !slices.Equal(resultNames, tc.expected) {
				t.Errorf("expected %v, got %v", tc.expected, resultNames)
			}
		})
	}
}

// makeTestArtifacts creates artifacts with parsed versions by going through
// updates.ParseIndex, as the parsed version of an artifact is not exported.
func makeTestArtifacts(t *testing.T, files map[string]string) []*updates.Artifact {
	t.Helper()

	index := updates.Index{Name: "test", Version: "1.0.0"}
	for filename, fileVersion := range files {
		index.Artifacts = append(index.Artifacts, &updates.Artifact{
			Filename: filename,
			Version:  fileVersion,
		})
	}

	indexJSON, err := json.Marshal(&index)
	if err != nil {
		t.Fatalf("failed to marshal test index: %s", err)
	}

	parsed, err := updates.ParseIndex(indexJSON, "", nil)
	if err != nil {
		t.Fatalf("failed to parse test index: %s", err)
	}

	return parsed.Artifacts
}
