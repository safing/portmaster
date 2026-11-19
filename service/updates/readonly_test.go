//go:build !windows

package updates

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCheckDirWritable(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	err := checkDirWritable(dir)
	if err != nil {
		t.Fatalf("writable dir reported as not writable: %s", err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Fatalf("check left %d files behind", len(entries))
	}

	if os.Geteuid() == 0 {
		t.Skip("root can write to read-only directories")
	}
	readOnly := filepath.Join(dir, "ro")
	err = os.Mkdir(readOnly, 0o500)
	if err != nil {
		t.Fatal(err)
	}
	err = checkDirWritable(readOnly)
	if err == nil {
		t.Fatal("read-only dir reported as writable")
	}
}
