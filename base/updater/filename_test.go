package updater

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
)

func testRegexMatch(t *testing.T, testRegex *regexp.Regexp, testString string, shouldMatch bool) {
	t.Helper()

	if testRegex.MatchString(testString) != shouldMatch {
		if shouldMatch {
			t.Errorf("regex %s should match %s", testRegex, testString)
		} else {
			t.Errorf("regex %s should not match %s", testRegex, testString)
		}
	}
}

func testRegexFind(t *testing.T, testRegex *regexp.Regexp, testString string, shouldMatch bool) {
	t.Helper()

	if (testRegex.FindString(testString) != "") != shouldMatch {
		if shouldMatch {
			t.Errorf("regex %s should find %s", testRegex, testString)
		} else {
			t.Errorf("regex %s should not find %s", testRegex, testString)
		}
	}
}

func testVersionTransformation(t *testing.T, testFilename, testIdentifier, testVersion string) {
	t.Helper()

	identifier, version, ok := GetIdentifierAndVersion(testFilename)
	if !ok {
		t.Errorf("failed to get identifier and version of %s", testFilename)
	}
	assert.Equal(t, testIdentifier, identifier, "identifier does not match")
	assert.Equal(t, testVersion, version, "version does not match")

	versionedPath := GetVersionedPath(testIdentifier, testVersion)
	assert.Equal(t, testFilename, versionedPath, "filename (versioned path) does not match")
}

func TestRegexes(t *testing.T) {
	t.Parallel()

	testRegexMatch(t, rawVersionRegex, "0.1.2", true)
	testRegexMatch(t, rawVersionRegex, "0.1.2-beta", true)
	testRegexMatch(t, rawVersionRegex, "0.1.2-staging", true)
	testRegexMatch(t, rawVersionRegex, "12.13.14", true)

	testRegexMatch(t, rawVersionRegex, "v0.1.2", false)
	testRegexMatch(t, rawVersionRegex, "0.", false)
	testRegexMatch(t, rawVersionRegex, "0.1", false)
	testRegexMatch(t, rawVersionRegex, "0.1.", false)
	testRegexMatch(t, rawVersionRegex, ".1.2", false)
	testRegexMatch(t, rawVersionRegex, ".1.", false)
	testRegexMatch(t, rawVersionRegex, "012345", false)

	testRegexFind(t, fileVersionRegex, "/path/to/file_v0-0-0", true)
	testRegexFind(t, fileVersionRegex, "/path/to/file_v1-2-3", true)
	testRegexFind(t, fileVersionRegex, "/path/to/file_v1-2-3.exe", true)

	testRegexFind(t, fileVersionRegex, "/path/to/file-v1-2-3", false)
	testRegexFind(t, fileVersionRegex, "/path/to/file_v1.2.3", false)
	testRegexFind(t, fileVersionRegex, "/path/to/file_1-2-3", false)
	testRegexFind(t, fileVersionRegex, "/path/to/file_v1-2", false)
	testRegexFind(t, fileVersionRegex, "/path/to/file-v1-2-3", false)

	testVersionTransformation(t, "/path/to/file_v0-0-0", "/path/to/file", "0.0.0")
	testVersionTransformation(t, "/path/to/file_v1-2-3", "/path/to/file", "1.2.3")
	testVersionTransformation(t, "/path/to/file_v1-2-3-beta", "/path/to/file", "1.2.3-beta")
	testVersionTransformation(t, "/path/to/file_v1-2-3-staging", "/path/to/file", "1.2.3-staging")
	testVersionTransformation(t, "/path/to/file_v1-2-3.exe", "/path/to/file.exe", "1.2.3")
	testVersionTransformation(t, "/path/to/file_v1-2-3-staging.exe", "/path/to/file.exe", "1.2.3-staging")
}
