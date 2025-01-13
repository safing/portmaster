//go:build !windows

package utils

import "os"

// SetDirPermission sets the permission of a directory.
func SetDirPermission(path string, perm FSPermission) error {
	return os.Chmod(path, perm.AsUnixDirExecPermission())
}

// SetExecPermission sets the permission of an executable file.
func SetExecPermission(path string, perm FSPermission) error {
	return SetDirPermission(path, perm)
}

// SetFilePermission sets the permission of a non executable file.
func SetFilePermission(path string, perm FSPermission) error {
	return os.Chmod(path, perm.AsUnixFilePermission())
}
