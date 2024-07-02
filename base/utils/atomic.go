package utils

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"

	"github.com/safing/portmaster/base/utils/renameio"
)

// AtomicFileOptions holds additional options for manipulating
// the behavior of CreateAtomic and friends.
type AtomicFileOptions struct {
	// Mode is the file mode for the new file. If
	// 0, the file mode will be set to 0600.
	Mode os.FileMode

	// TempDir is the path to the temp-directory
	// that should be used. If empty, it defaults
	// to the system temp.
	TempDir string
}

// CreateAtomic creates or overwrites a file at dest atomically using
// data from r. Atomic means that even in case of a power outage,
// dest will never be a zero-length file. It will always either contain
// the previous data (or not exist) or the new data but never anything
// in between.
func CreateAtomic(dest string, r io.Reader, opts *AtomicFileOptions) error {
	if opts == nil {
		opts = &AtomicFileOptions{}
	}

	tmpFile, err := renameio.TempFile(opts.TempDir, dest)
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	defer tmpFile.Cleanup() //nolint:errcheck

	if opts.Mode != 0 {
		if err := tmpFile.Chmod(opts.Mode); err != nil {
			return fmt.Errorf("failed to update mode bits of temp file: %w", err)
		}
	}

	if _, err := io.Copy(tmpFile, r); err != nil {
		return fmt.Errorf("failed to copy source file: %w", err)
	}

	if err := tmpFile.CloseAtomicallyReplace(); err != nil {
		return fmt.Errorf("failed to rename temp file to %q", dest)
	}

	return nil
}

// CopyFileAtomic is like CreateAtomic but copies content from
// src to dest. If opts.Mode is 0 CopyFileAtomic tries to set
// the file mode of src to dest.
func CopyFileAtomic(dest string, src string, opts *AtomicFileOptions) error {
	if opts == nil {
		opts = &AtomicFileOptions{}
	}

	if opts.Mode == 0 {
		stat, err := os.Stat(src)
		if err != nil {
			return err
		}
		opts.Mode = stat.Mode()
	}

	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() {
		_ = f.Close()
	}()

	return CreateAtomic(dest, f, opts)
}

// ReplaceFileAtomic replaces the file at dest with the content from src.
// If dest exists it's file mode copied and used for the replacement. If
// not, dest will get the same file mode as src. See CopyFileAtomic and
// CreateAtomic for more information.
func ReplaceFileAtomic(dest string, src string, opts *AtomicFileOptions) error {
	if opts == nil {
		opts = &AtomicFileOptions{}
	}

	if opts.Mode == 0 {
		stat, err := os.Stat(dest)
		if err == nil {
			opts.Mode = stat.Mode()
		} else if !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}

	return CopyFileAtomic(dest, src, opts)
}
