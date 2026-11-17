//go:build linux

package config

import (
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"testing"
)

// TestSaveConfigFailedWriteKeepsOldFile checks that a write that fails midway
// (here: exceeding RLIMIT_FSIZE, like a full disk) does not destroy the
// previously persisted config file.
func TestSaveConfigFailedWriteKeepsOldFile(t *testing.T) { //nolint:paralleltest // Changes process wide resource limits.
	options = make(map[string]*Option)
	quickRegister(t, "persist/a", OptTypeString, "default")
	quickRegister(t, "persist/b", OptTypeString, "default")

	oldPath := configFilePath
	configFilePath = filepath.Join(t.TempDir(), "config.json")
	defer func() { configFilePath = oldPath }()

	// Persist an initial config.
	err := setConfigOption("persist/a", "original-value-that-must-survive", false)
	if err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(configFilePath)
	if err != nil {
		t.Fatal(err)
	}

	// Make every write beyond a few bytes fail, like a full disk would.
	signal.Ignore(syscall.SIGXFSZ)
	defer signal.Reset(syscall.SIGXFSZ)
	var oldLimit syscall.Rlimit
	err = syscall.Getrlimit(syscall.RLIMIT_FSIZE, &oldLimit)
	if err != nil {
		t.Fatal(err)
	}
	newLimit := oldLimit
	newLimit.Cur = 16
	err = syscall.Setrlimit(syscall.RLIMIT_FSIZE, &newLimit)
	if err != nil {
		t.Fatal(err)
	}
	saveErr := setConfigOption("persist/b", "new-value", false)
	err = syscall.Setrlimit(syscall.RLIMIT_FSIZE, &oldLimit)
	if err != nil {
		t.Fatal(err)
	}
	if saveErr == nil {
		t.Fatal("expected save to fail")
	}

	current, err := os.ReadFile(configFilePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(current) != string(original) {
		t.Fatalf("failed save corrupted config file: was %q, now %q", original, current)
	}
}
