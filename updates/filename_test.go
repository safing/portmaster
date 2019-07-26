package updates

import (
	"regexp"
	"testing"
)

func testRegexMatch(t *testing.T, testRegex *regexp.Regexp, testString string, shouldMatch bool) {
	if testRegex.MatchString(testString) != shouldMatch {
		if shouldMatch {
			t.Errorf("regex %s should match %s", testRegex, testString)
		} else {
			t.Errorf("regex %s should not match %s", testRegex, testString)
		}
	}
}

func testRegexFind(t *testing.T, testRegex *regexp.Regexp, testString string, shouldMatch bool) {
	if (testRegex.FindString(testString) != "") != shouldMatch {
		if shouldMatch {
			t.Errorf("regex %s should find %s", testRegex, testString)
		} else {
			t.Errorf("regex %s should not find %s", testRegex, testString)
		}
	}
}

func TestRegexes(t *testing.T) {
	testRegexMatch(t, rawVersionRegex, "0.1.2", true)
	testRegexMatch(t, rawVersionRegex, "0.1.2*", true)
	testRegexMatch(t, rawVersionRegex, "0.1.2b", true)
	testRegexMatch(t, rawVersionRegex, "0.1.2b*", true)
	testRegexMatch(t, rawVersionRegex, "12.13.14", true)

	testRegexMatch(t, rawVersionRegex, "v0.1.2", false)
	testRegexMatch(t, rawVersionRegex, "0.", false)
	testRegexMatch(t, rawVersionRegex, "0.1", false)
	testRegexMatch(t, rawVersionRegex, "0.1.", false)
	testRegexMatch(t, rawVersionRegex, ".1.2", false)
	testRegexMatch(t, rawVersionRegex, ".1.", false)
	testRegexMatch(t, rawVersionRegex, "012345", false)

	testRegexFind(t, fileVersionRegex, "/path/to/file_v1-2-3", true)
	testRegexFind(t, fileVersionRegex, "/path/to/file_v1-2-3.exe", true)

	testRegexFind(t, fileVersionRegex, "/path/to/file-v1-2-3", false)
	testRegexFind(t, fileVersionRegex, "/path/to/file_v1.2.3", false)
	testRegexFind(t, fileVersionRegex, "/path/to/file_1-2-3", false)
	testRegexFind(t, fileVersionRegex, "/path/to/file_v1-2", false)
	testRegexFind(t, fileVersionRegex, "/path/to/file-v1-2-3", false)
}
