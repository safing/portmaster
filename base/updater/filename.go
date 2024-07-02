package updater

import (
	"path"
	"regexp"
	"strings"
)

var (
	fileVersionRegex = regexp.MustCompile(`_v[0-9]+-[0-9]+-[0-9]+(-[a-z]+)?`)
	rawVersionRegex  = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(-[a-z]+)?$`)
)

// GetIdentifierAndVersion splits the given file path into its identifier and version.
func GetIdentifierAndVersion(versionedPath string) (identifier, version string, ok bool) {
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

// GetVersionedPath combines the identifier and version and returns it as a file path.
func GetVersionedPath(identifier, version string) (versionedPath string) {
	identifierPath, filename := path.Split(identifier)

	// Split the filename where the version should go.
	splittedFilename := strings.SplitN(filename, ".", 2)
	// Replace `.` with `-` for the filename format.
	transformedVersion := strings.Replace(version, ".", "-", 2)

	// Put everything back together and return it.
	versionedPath = identifierPath + splittedFilename[0] + "_v" + transformedVersion
	if len(splittedFilename) > 1 {
		versionedPath += "." + splittedFilename[1]
	}
	return versionedPath
}
