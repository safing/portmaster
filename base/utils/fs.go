package utils

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"runtime"

	"github.com/hectane/go-acl"
)

const isWindows = runtime.GOOS == "windows"

// EnsureDirectory ensures that the given directory exists and that is has the given permissions set.
// If path is a file, it is deleted and a directory created.
func EnsureDirectory(path string, perm os.FileMode) error {
	// open path
	f, err := os.Stat(path)
	if err == nil {
		// file exists
		if f.IsDir() {
			// directory exists, check permissions
			if isWindows {
				// Ignore windows permission error. For none admin users it will always fail.
				acl.Chmod(path, perm)
				return nil
			} else if f.Mode().Perm() != perm {
				return os.Chmod(path, perm)
			}
			return nil
		}
		err = os.Remove(path)
		if err != nil {
			return fmt.Errorf("could not remove file %s to place dir: %w", path, err)
		}
	}
	// file does not exist (or has been deleted)
	if err == nil || errors.Is(err, fs.ErrNotExist) {
		err = os.Mkdir(path, perm)
		if err != nil {
			return fmt.Errorf("could not create dir %s: %w", path, err)
		}
		if isWindows {
			// Ignore windows permission error. For none admin users it will always fail.
			acl.Chmod(path, perm)
			return nil
		} else {
			return os.Chmod(path, perm)
		}
	}
	// other error opening path
	return fmt.Errorf("failed to access %s: %w", path, err)
}

// PathExists returns whether the given path (file or dir) exists.
func PathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil || errors.Is(err, fs.ErrExist)
}
