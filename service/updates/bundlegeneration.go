package updates

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/gobwas/glob"
	semver "github.com/hashicorp/go-version"
)

type BundleFileSettings struct {
	Name            string
	Version         string
	PrimaryArtifact string
	BaseURL         string

	Templates   map[string]Artifact
	IgnoreFiles []string
	UnpackFiles map[string]string

	cleanedBaseURL   string
	ignoreFilesGlobs []glob.Glob
	unpackFilesGlobs map[string]glob.Glob
}

func (bs *BundleFileSettings) init() error {
	// Transform base URL into expected format.
	bs.cleanedBaseURL = strings.TrimSuffix(bs.BaseURL, "/") + "/"

	// Parse ignore files patterns.
	bs.ignoreFilesGlobs = make([]glob.Glob, 0, len(bs.IgnoreFiles))
	for _, pattern := range bs.IgnoreFiles {
		g, err := glob.Compile(pattern, os.PathSeparator)
		if err != nil {
			return fmt.Errorf("invalid ingore files pattern %q: %w", pattern, err)
		}
		bs.ignoreFilesGlobs = append(bs.ignoreFilesGlobs, g)
	}

	// Parse unpack files patterns.
	bs.unpackFilesGlobs = make(map[string]glob.Glob)
	for setting, pattern := range bs.UnpackFiles {
		g, err := glob.Compile(pattern, os.PathSeparator)
		if err != nil {
			return fmt.Errorf("invalid unpack files pattern %q: %w", pattern, err)
		}
		bs.unpackFilesGlobs[setting] = g
	}

	return nil
}

// IsIgnored returns whether a filename should be ignored.
func (bs *BundleFileSettings) IsIgnored(filename string) bool {
	for _, ignoreGlob := range bs.ignoreFilesGlobs {
		if ignoreGlob.Match(filename) {
			return true
		}
	}

	return false
}

// UnpackSetting returns the unpack setings for the given filename.
func (bs *BundleFileSettings) UnpackSetting(filename string) (string, error) {
	var foundSetting string

settings:
	for unpackSetting, matchGlob := range bs.unpackFilesGlobs {
		switch {
		case !matchGlob.Match(filename):
			// Check next if glob does not match.
			continue settings
		case foundSetting == "":
			// First find, save setting.
			foundSetting = unpackSetting
		case foundSetting != unpackSetting:
			// Additional find, and setting is not the same.
			return "", errors.New("matches contradicting unpack settings")
		}
	}

	return foundSetting, nil
}

// GenerateBundleFromDir generates a bundle from a given folder.
func GenerateBundleFromDir(bundleDir string, settings BundleFileSettings) (*Bundle, error) {
	artifacts := make(map[string]Artifact)

	// Initialize.
	err := settings.init()
	if err != nil {
		return nil, fmt.Errorf("invalid bundle settings: %w", err)
	}
	bundleDir, err = filepath.Abs(bundleDir)
	if err != nil {
		return nil, fmt.Errorf("invalid bundle dir: %w", err)
	}

	err = filepath.WalkDir(bundleDir, func(fullpath string, d fs.DirEntry, err error) error {
		// Fail on access error.
		if err != nil {
			return err
		}

		// Step 1: Extract information and check ignores.

		// Skip folders.
		if d.IsDir() {
			return nil
		}

		// Get relative path for processing.
		relpath, err := filepath.Rel(bundleDir, fullpath)
		if err != nil {
			return fmt.Errorf("invalid relative path for %s: %w", fullpath, err)
		}

		// Check if file is in the ignore list.
		if settings.IsIgnored(relpath) {
			return nil
		}

		// Extract version, if present.
		identifier, version, ok := getIdentifierAndVersion(d.Name())
		if !ok {
			// Fallback to using filename as identifier, which is normal for the simplified system.
			identifier = d.Name()
			version = ""
		}
		var versionNum *semver.Version
		if version != "" {
			versionNum, err = semver.NewVersion(version)
			if err != nil {
				return fmt.Errorf("invalid version %s for %s: %w", relpath, version, err)
			}
		}

		// Extract platform.
		platform := "all"
		before, _, found := strings.Cut(relpath, string(os.PathSeparator))
		if found {
			platform = before
		}

		// Step 2: Check and compare file version.

		// Make the key platform specific since there can be same filename for multiple platforms.
		key := platform + "/" + identifier
		existing, ok := artifacts[key]
		if ok {
			// Check for duplicates and mixed versioned/non-versioned.
			switch {
			case existing.Version == version:
				return fmt.Errorf("duplicate version for %s: %s and %s", key, existing.localFile, fullpath)
			case (existing.Version == "") != (version == ""):
				return fmt.Errorf("both a versioned and non-versioned file for: %s: %s and %s", key, existing.localFile, fullpath)
			}

			// Compare versions.
			existingVersion, _ := semver.NewVersion(existing.Version)
			switch {
			case existingVersion.Equal(versionNum):
				return fmt.Errorf("duplicate version for %s: %s and %s", key, existing.localFile, fullpath)
			case existingVersion.GreaterThan(versionNum):
				// New version is older, skip.
				return nil
			}
		}

		// Step 3: Create new Artifact.

		artifact := Artifact{}

		// Check if the caller provided a template for the artifact.
		if t, ok := settings.Templates[identifier]; ok {
			artifact = t
		}

		// Set artifact properties.
		if artifact.Filename == "" {
			artifact.Filename = identifier
		}
		if len(artifact.URLs) == 0 && settings.BaseURL != "" {
			artifact.URLs = []string{settings.cleanedBaseURL + relpath}
		}
		if artifact.Platform == "" {
			artifact.Platform = platform
		}
		if artifact.Unpack == "" {
			unpackSetting, err := settings.UnpackSetting(relpath)
			if err != nil {
				return fmt.Errorf("invalid unpack setting for %s at %s: %w", key, relpath, err)
			}
			artifact.Unpack = unpackSetting
		}
		if artifact.Version == "" {
			artifact.Version = version
		}

		// Remove unpack suffix.
		if artifact.Unpack != "" {
			artifact.Filename, _ = strings.CutSuffix(artifact.Filename, "."+artifact.Unpack)
		}

		// Set local file path.
		artifact.localFile = fullpath

		// Save new artifact to map.
		artifacts[key] = artifact
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scanning dir: %w", err)
	}

	// Create base bundle.
	bundle := &Bundle{
		Name:      settings.Name,
		Version:   settings.Version,
		Published: time.Now(),
	}
	if bundle.Version == "" && settings.PrimaryArtifact != "" {
		pv, ok := artifacts[settings.PrimaryArtifact]
		if ok {
			bundle.Version = pv.Version
		}
	}
	if bundle.Name == "" {
		bundle.Name = strings.Trim(filepath.Base(bundleDir), "./\\")
	}

	// Convert to slice and compute hashes.
	export := make([]Artifact, 0, len(artifacts))
	for _, artifact := range artifacts {
		// Compute hash.
		hash, err := getSHA256(artifact.localFile, artifact.Unpack)
		if err != nil {
			return nil, fmt.Errorf("calculate hash of file: %s %w", artifact.localFile, err)
		}
		artifact.SHA256 = hash

		// Remove "all" platform IDs.
		if artifact.Platform == "all" {
			artifact.Platform = ""
		}

		// Remove default versions.
		if artifact.Version == bundle.Version {
			artifact.Version = ""
		}

		// Add to export slice.
		export = append(export, artifact)
	}

	// Sort final artifacts.
	slices.SortFunc(export, func(a, b Artifact) int {
		switch {
		case a.Filename != b.Filename:
			return strings.Compare(a.Filename, b.Filename)
		case a.Platform != b.Platform:
			return strings.Compare(a.Platform, b.Platform)
		case a.Version != b.Version:
			return strings.Compare(a.Version, b.Version)
		case a.SHA256 != b.SHA256:
			return strings.Compare(a.SHA256, b.SHA256)
		default:
			return 0
		}
	})

	// Assign and return.
	bundle.Artifacts = export
	return bundle, nil
}

func selectLatestArtifacts(artifacts []Artifact) ([]Artifact, error) {
	artifactsMap := make(map[string]Artifact)

	for _, a := range artifacts {
		// Make the key platform specific since there can be same filename for multiple platforms.
		key := a.Filename + a.Platform
		aMap, ok := artifactsMap[key]
		if !ok {
			artifactsMap[key] = a
			continue
		}

		if aMap.Version == "" || a.Version == "" {
			return nil, fmt.Errorf("invalid mix version and non versioned files for: %s", a.Filename)
		}

		mapVersion, err := semver.NewVersion(aMap.Version)
		if err != nil {
			return nil, fmt.Errorf("invalid version for artifact: %s", aMap.Filename)
		}

		artifactVersion, err := semver.NewVersion(a.Version)
		if err != nil {
			return nil, fmt.Errorf("invalid version for artifact: %s", a.Filename)
		}

		if mapVersion.LessThan(artifactVersion) {
			artifactsMap[key] = a
		}
	}

	artifactsFiltered := make([]Artifact, 0, len(artifactsMap))
	for _, a := range artifactsMap {
		artifactsFiltered = append(artifactsFiltered, a)
	}

	return artifactsFiltered, nil
}

func getSHA256(path string, unpackType string) (string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}

	// Decompress if compression was applied to the file.
	if unpackType != "" {
		content, err = decompress(unpackType, content)
		if err != nil {
			return "", err
		}
	}

	// Calculate hash
	hash := sha256.Sum256(content)
	return hex.EncodeToString(hash[:]), nil
}

var fileVersionRegex = regexp.MustCompile(`_v[0-9]+-[0-9]+-[0-9]+(-[a-z]+)?`)

func getIdentifierAndVersion(versionedPath string) (identifier, version string, ok bool) {
	dirPath, filename := path.Split(versionedPath)

	// Extract version from filename.
	rawVersion := fileVersionRegex.FindString(filename)
	if rawVersion == "" {
		// No version present in file, making it invalid.
		return "", "", false
	}

	// Trim the `_v` that gets caught by the regex and
	// replace `-` with `.` to get the version string.
	version = strings.Replace(strings.TrimLeft(rawVersion, "_v"), "-", ".", 2)

	// Put the filename back together without version.
	i := strings.Index(filename, rawVersion)
	if i < 0 {
		// extracted version not in string (impossible)
		return "", "", false
	}
	filename = filename[:i] + filename[i+len(rawVersion):]

	// Put the full path back together and return it.
	// `dirPath + filename` is guaranteed by path.Split()
	return dirPath + filename, version, true
}

// GenerateMockFolder generates mock bundle folder for testing.
func GenerateMockFolder(dir, name, version string) error {
	// Make sure dir exists
	_ = os.MkdirAll(dir, defaultDirMode)

	// Create empty files
	file, err := os.Create(filepath.Join(dir, "portmaster"))
	if err != nil {
		return err
	}
	_ = file.Close()
	file, err = os.Create(filepath.Join(dir, "portmaster-core"))
	if err != nil {
		return err
	}
	_ = file.Close()
	file, err = os.Create(filepath.Join(dir, "portmaster.zip"))
	if err != nil {
		return err
	}
	_ = file.Close()
	file, err = os.Create(filepath.Join(dir, "assets.zip"))
	if err != nil {
		return err
	}
	_ = file.Close()

	bundle, err := GenerateBundleFromDir(dir, BundleFileSettings{
		Name:    name,
		Version: version,
	})
	if err != nil {
		return err
	}

	bundleStr, err := json.MarshalIndent(bundle, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to marshal bundle: %s\n", err)
	}

	err = os.WriteFile(filepath.Join(dir, "index.json"), bundleStr, defaultFileMode)
	if err != nil {
		return err
	}
	return nil
}
