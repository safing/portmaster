// Fixtures for the Rust port of service/updates.
//
// The index model, its validation and the upgrade decisions are generated with
// the real Go package, so the Rust implementation is compared against the
// actual behavior instead of a description of it.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	semver "github.com/hashicorp/go-version"

	"github.com/safing/portmaster/service/updates"
)

func genUpdates() {
	published := time.Unix(1_700_000_000, 0).UTC()

	// A representative index with a platform-independent artifact, a
	// platform-specific one and one for another platform.
	indexJSON, err := json.Marshal(map[string]any{
		"Name":      "test-index",
		"Version":   "v1.2.3",
		"Published": published.Format(time.RFC3339),
		"Artifacts": []map[string]any{
			{
				"Filename": "base.dsdl",
				"SHA256":   strings.Repeat("a", 64),
				"URLs":     []string{"https://example.com/base.dsdl"},
			},
			{
				"Filename": "linux.zip",
				"SHA256":   strings.Repeat("b", 64),
				"URLs":     []string{"https://example.com/linux.zip"},
				"Platform": "linux_amd64",
				"Unpack":   "zip",
				"Version":  "v1.2.2",
			},
			{
				"Filename": "windows.zip",
				"SHA256":   strings.Repeat("c", 64),
				"URLs":     []string{"https://example.com/windows.zip"},
				"Platform": "windows_amd64",
			},
		},
	})
	if err != nil {
		fail(err)
	}

	type artifactFixture struct {
		Filename string   `json:"filename"`
		SHA256   string   `json:"sha256"`
		URLs     []string `json:"urls"`
		Platform string   `json:"platform"`
		Unpack   string   `json:"unpack"`
		Version  string   `json:"version"`
	}
	type indexFixture struct {
		Name      string            `json:"name"`
		Version   string            `json:"version"`
		Published string            `json:"published"`
		Artifacts []artifactFixture `json:"artifacts"`
	}
	makeIndexFixture := func(index *updates.Index) indexFixture {
		fixture := indexFixture{
			Name:      index.Name,
			Version:   index.Version,
			Published: index.Published.UTC().Format(time.RFC3339),
		}
		for _, artifact := range index.Artifacts {
			fixture.Artifacts = append(fixture.Artifacts, artifactFixture{
				Filename: artifact.Filename,
				SHA256:   artifact.SHA256,
				URLs:     artifact.URLs,
				Platform: artifact.Platform,
				Unpack:   artifact.Unpack,
				Version:  artifact.Version,
			})
		}
		return fixture
	}

	linuxIndex, err := updates.ParseIndex(indexJSON, "linux_amd64", nil)
	if err != nil {
		fail(err)
	}
	windowsIndex, err := updates.ParseIndex(indexJSON, "windows_amd64", nil)
	if err != nil {
		fail(err)
	}

	// Artifact filename validation through the real parser.
	filenameCases := []string{
		"filterlists.zip", "geoipv4.mmdb", "portmaster-core", "base.dsdl",
		"index.dsd", "file.with.dots.tar.gz", "file_with_underscores",
		"file-with-dashes", "", ".", "..", "../evil", "./evil", "a/../evil",
		"dir/file", "dir\\file", "//evil", "evil/", "C:file", "file:stream",
	}
	type filenameCase struct {
		Name  string `json:"name"`
		OK    bool   `json:"ok"`
		Error string `json:"error,omitempty"`
	}
	filenames := make([]filenameCase, 0, len(filenameCases))
	for _, name := range filenameCases {
		content, err := json.Marshal(map[string]any{
			"Name":      "filename-test",
			"Version":   "v1.0.0",
			"Published": published.Format(time.RFC3339),
			"Artifacts": []map[string]any{{"Filename": name, "SHA256": strings.Repeat("d", 64)}},
		})
		if err != nil {
			fail(err)
		}
		_, err = updates.ParseIndex(content, "linux_amd64", nil)
		c := filenameCase{Name: name, OK: err == nil}
		if err != nil {
			c.Error = err.Error()
		}
		filenames = append(filenames, c)
	}

	// Upgrade eligibility and decisions, using real indexes.
	makeIndex := func(name, version, publishedAt string) *updates.Index {
		var publishedField any
		if publishedAt != "" {
			publishedField = publishedAt
		}
		content, err := json.Marshal(map[string]any{
			"Name":      name,
			"Version":   version,
			"Published": publishedField,
			"Artifacts": []map[string]any{},
		})
		if err != nil {
			fail(err)
		}
		index, err := updates.ParseIndex(content, "linux_amd64", nil)
		if err != nil {
			fail(err)
		}
		return index
	}
	now := time.Now().UTC()
	recent := now.Add(-5 * time.Minute).Format(time.RFC3339)
	older := now.Add(-2 * time.Hour).Format(time.RFC3339)
	future := now.Add(time.Hour).Format(time.RFC3339)

	type eligibilityCase struct {
		Name      string `json:"name"`
		IndexJSON string `json:"index_json"`
		OK        bool   `json:"ok"`
		Error     string `json:"error,omitempty"`
	}
	eligibility := []eligibilityCase{}
	addEligibility := func(name, indexName, version, publishedAt string) {
		fields := map[string]any{
			"Name":      indexName,
			"Version":   version,
			"Artifacts": []map[string]any{},
		}
		if publishedAt != "" {
			fields["Published"] = publishedAt
		}
		content, err := json.Marshal(fields)
		if err != nil {
			fail(err)
		}
		index, err := updates.ParseIndex(content, "linux_amd64", nil)
		if err != nil {
			fail(err)
		}
		err = index.CanDoUpgrades()
		c := eligibilityCase{Name: name, IndexJSON: string(content), OK: err == nil}
		if err != nil {
			c.Error = err.Error()
		}
		eligibility = append(eligibility, c)
	}
	addEligibility("ok", "test-index", "v1.2.3", recent)
	addEligibility("missing-version", "test-index", "", recent)
	addEligibility("missing-publish-date", "test-index", "v1.2.3", "")
	addEligibility("future", "test-index", "v1.2.3", future)

	type upgradeCase struct {
		Name        string `json:"name"`
		CurrentJSON string `json:"current_json"`
		NewJSON     string `json:"new_json"`
		OK          bool   `json:"ok"`
		Error       string `json:"error,omitempty"`
	}
	upgradeCases := []upgradeCase{}
	addUpgradeCase := func(name string, current, new *updates.Index) {
		currentContent, err := json.Marshal(current)
		if err != nil {
			fail(err)
		}
		newContent, err := json.Marshal(new)
		if err != nil {
			fail(err)
		}
		err = current.ShouldUpgradeTo(new)
		c := upgradeCase{
			Name:        name,
			CurrentJSON: string(currentContent),
			NewJSON:     string(newContent),
			OK:          err == nil,
		}
		if err != nil {
			c.Error = err.Error()
		}
		upgradeCases = append(upgradeCases, c)
	}
	addUpgradeCase("upgrade",
		makeIndex("test-index", "v1.0.0", older),
		makeIndex("test-index", "v1.1.0", recent))
	addUpgradeCase("same-index",
		makeIndex("test-index", "v1.2.3", recent),
		makeIndex("test-index", "v1.2.3", recent))
	addUpgradeCase("name-mismatch",
		makeIndex("test-index", "v1.2.3", recent),
		makeIndex("other-index", "v1.2.4", recent))
	addUpgradeCase("newer-version-older-date",
		makeIndex("test-index", "v1.2.3", recent),
		makeIndex("test-index", "v1.2.4", older))
	addUpgradeCase("older-version-newer-date",
		makeIndex("test-index", "v1.2.3", older),
		makeIndex("test-index", "v1.2.2", recent))
	addUpgradeCase("breaking-downgrade",
		makeIndex("test-index", "v2.0.0", older),
		makeIndex("test-index", "v1.9.0", older))
	addUpgradeCase("downgrade",
		makeIndex("test-index", "v1.9.0", older),
		makeIndex("test-index", "v1.8.0", older))
	addUpgradeCase("bootstrap",
		makeIndex("test-index", "0.0.0", older),
		makeIndex("other-index", "v9.9.9", recent))
	addUpgradeCase("current-cannot-upgrade",
		makeIndex("test-index", "", older),
		makeIndex("test-index", "v1.0.0", recent))

	// Version parsing and comparison, using hashicorp/go-version.
	versions := []string{
		"1.2.3", "v1.2.3", "1.2", "1", "1.2.3-rc1", "1.2.3-alpha.1",
		"1.2.3+build.5", "1.2.3-rc1+build", "v0.6.4", "0.0.0",
		"", "v", "1.2.3.4", "1..3", "1.2.x", "-1.2.3",
		"1.2.4", "1.2.0", "1.0.0", "1.9.9", "2.0.0", "0.6.4",
		"1.2.3-rc2", "1.2.3-alpha.2",
	}
	type versionCase struct {
		Input  string `json:"input"`
		OK     bool   `json:"ok"`
		Parsed string `json:"parsed,omitempty"`
	}
	versionCases := make([]versionCase, 0, len(versions))
	parsedVersions := make(map[string]*semver.Version)
	for _, input := range versions {
		version, err := semver.NewVersion(input)
		c := versionCase{Input: input, OK: err == nil}
		if err == nil {
			c.Parsed = version.String()
			parsedVersions[input] = version
		}
		versionCases = append(versionCases, c)
	}

	type compareCase struct {
		A      string `json:"a"`
		B      string `json:"b"`
		Result int    `json:"result"`
	}
	comparePairs := [][2]string{
		{"1.2.3", "1.2.3"}, {"1.2.3", "1.2.4"}, {"1.2.4", "1.2.3"},
		{"1.2", "1.2.0"}, {"1", "1.0.0"}, {"1.2.3-rc1", "1.2.3"},
		{"1.2.3-rc1", "1.2.3-rc2"}, {"1.2.3-alpha.1", "1.2.3-alpha.2"},
		{"v0.6.4", "0.6.4"}, {"2.0.0", "1.9.9"},
	}
	comparisons := make([]compareCase, 0, len(comparePairs))
	for _, pair := range comparePairs {
		a, okA := parsedVersions[pair[0]]
		b, okB := parsedVersions[pair[1]]
		if !okA || !okB {
			fail(fmt.Errorf("updates: missing parsed version for %q/%q", pair[0], pair[1]))
		}
		comparisons = append(comparisons, compareCase{A: pair[0], B: pair[1], Result: a.Compare(b)})
	}

	// SHA256 checks.
	shaOK := sha256Hex([]byte("portmaster"))
	shaCases := []map[string]any{
		{"name": "ok", "data": "portmaster", "sha256": shaOK, "ok": true},
		{"name": "mismatch", "data": "portmaster", "sha256": sha256Hex([]byte("other")), "ok": false},
		{"name": "invalid-hex", "data": "portmaster", "sha256": "zz", "ok": false},
		{"name": "wrong-size", "data": "portmaster", "sha256": "abcd", "ok": false},
	}

	writeObject("updates.json", map[string]any{
		"index_json": string(indexJSON),
		"parsed": map[string]any{
			"linux_amd64":   makeIndexFixture(linuxIndex),
			"windows_amd64": makeIndexFixture(windowsIndex),
		},
		"filenames":     filenames,
		"eligibility":   eligibility,
		"upgrade_cases": upgradeCases,
		"versions":      versionCases,
		"comparisons":   comparisons,
		"sha256":        shaCases,
	})
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
