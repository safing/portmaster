package log

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// TestNewFileWriterPermissions checks that log files are only accessible by
// their owner, even if the log directory already exists with broader
// permissions.
func TestNewFileWriterPermissions(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("unix file permissions do not apply on Windows")
	}

	// Simulate a log directory that already exists and is readable by others.
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	writer, err := NewFileWriter(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected one log file, got %d", len(entries))
	}
	info, err := entries[0].Info()
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("log file is created with permissions %o, expected 600", perm)
	}
}

// TestNewFileWriterExistingFile checks that an existing log file, which is
// reused when the writer starts within the same second, is restricted too.
func TestNewFileWriterExistingFile(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("unix file permissions do not apply on Windows")
	}

	// Create readable log files for the next seconds, so that the writer
	// reuses one of them.
	dir := t.TempDir()
	now := time.Now().UTC()
	for i := range 3 {
		logFile := now.Add(time.Duration(i)*time.Second).Format("2006-01-02_15-04-05") + ".log"
		path := filepath.Join(dir, logFile)
		if err := os.WriteFile(path, nil, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	writer, err := NewFileWriter(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("expected the writer to reuse an existing log file, got %d files", len(entries))
	}
	info, err := os.Stat(writer.file.Name())
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("existing log file has permissions %o, expected 600", perm)
	}
}
