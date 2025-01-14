//go:build !windows

package utils

import "os"

// SetFilePermission sets the permission of a file or directory.
func SetFilePermission(path string, perm FSPermission) error {
	return os.Chmod(path, perm.AsUnixPermission())
}
