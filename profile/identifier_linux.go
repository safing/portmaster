package profile

import (
	"path/filepath"
	"strings"

	"github.com/Safing/portbase/utils"
)

// GetPathIdentifier returns the identifier from the given path
func GetPathIdentifier(path string) string {
	// clean path
	// TODO: is this necessary?
	cleanedPath, err := filepath.EvalSymlinks(path)
	if err == nil {
		path = cleanedPath
	} else {
		path = filepath.Clean(path)
	}

	splittedPath := strings.Split(path, "/")

	// strip sensitive data
	switch {
	case strings.HasPrefix(path, "/home/"):
		splittedPath = splittedPath[3:]
	case strings.HasPrefix(path, "/root/"):
		splittedPath = splittedPath[2:]
	}

	// common directories with executable
	if i := utils.IndexOfString(splittedPath, "bin"); i > 0 {
		splittedPath = splittedPath[i:]
		return strings.Join(splittedPath, "/")
	}
	if i := utils.IndexOfString(splittedPath, "sbin"); i > 0 {
		splittedPath = splittedPath[i:]
		return strings.Join(splittedPath, "/")
	}

	// shorten to max 3
	if len(splittedPath) > 3 {
		splittedPath = splittedPath[len(splittedPath)-3:]
	}

	return strings.Join(splittedPath, "/")
}
