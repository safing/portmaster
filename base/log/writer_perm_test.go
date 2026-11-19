//go:build !windows

package log

import (
	"os"
	"testing"
)

func TestNewFileWriterPermissions(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	// The directory exists with broad permissions, e.g. created by a package.
	err := os.Chmod(dir, 0o755) //nolint:gosec // test setup
	if err != nil {
		t.Fatal(err)
	}
	w, err := NewFileWriter(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	info, err := w.file.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("log file permissions are %o, want 600", perm)
	}
}
