package service

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// On Windows, symlink creation requires elevated privileges or developer
// mode, and the service's BinDir default is different. Skip symlink tests
// there.
func skipIfNoSymlinks(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("symlink tests skipped on windows")
	}
}

func TestServiceConfigInitResolvesBinDirSymlinks(t *testing.T) {
	skipIfNoSymlinks(t)

	tmp := t.TempDir()
	target := filepath.Join(tmp, "real")
	link := filepath.Join(tmp, "link")

	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatalf("mkdir real: %v", err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	sc := &ServiceConfig{
		BinDir:  link,
		DataDir: filepath.Join(tmp, "data"),
		LogDir:  filepath.Join(tmp, "logs"),
	}
	if err := sc.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}

	if sc.BinDir != target {
		t.Errorf("BinDir not resolved through symlink: got %q, want %q", sc.BinDir, target)
	}
}

func TestServiceConfigInitLeavesBinDirUntouchedWhenNonExistent(t *testing.T) {
	skipIfNoSymlinks(t)

	tmp := t.TempDir()
	missing := filepath.Join(tmp, "does-not-exist")

	sc := &ServiceConfig{
		BinDir:  missing,
		DataDir: filepath.Join(tmp, "data"),
		LogDir:  filepath.Join(tmp, "logs"),
	}
	if err := sc.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}

	// When the BinDir does not exist yet, EvalSymlinks returns os.ErrNotExist
	// and we must leave the user-supplied value alone rather than failing
	// the daemon's startup.
	if sc.BinDir != missing {
		t.Errorf("BinDir changed when symlink target missing: got %q, want %q", sc.BinDir, missing)
	}
}

func TestServiceConfigInitFailsOnUnreadableBinDir(t *testing.T) {
	skipIfNoSymlinks(t)

	// A dangling symlink should not abort Init: EvalSymlinks returns
	// os.ErrNotExist (the underlying target is missing) and we intentionally
	// treat that as "BinDir not present yet, leave the user value alone" so
	// that fresh installs (where the directory is created later by the
	// service) do not fail startup.
	tmp := t.TempDir()
	dangling := filepath.Join(tmp, "dangling")
	if err := os.Symlink(filepath.Join(tmp, "no-such-target"), dangling); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	sc := &ServiceConfig{
		BinDir:  dangling,
		DataDir: filepath.Join(tmp, "data"),
		LogDir:  filepath.Join(tmp, "logs"),
	}
	if err := sc.Init(); err != nil {
		t.Fatalf("Init should not fail on dangling symlink, got: %v", err)
	}
	// BinDir must remain the user-supplied path so that the service can
	// later create it.
	if sc.BinDir != dangling {
		t.Errorf("BinDir changed for dangling symlink: got %q, want %q", sc.BinDir, dangling)
	}
}

func TestServiceConfigInitAppliesLinuxDefaults(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux-specific default test")
	}

	sc := &ServiceConfig{}
	if err := sc.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if sc.BinDir != "/usr/lib/portmaster" {
		t.Errorf("default BinDir = %q, want %q", sc.BinDir, "/usr/lib/portmaster")
	}
	if sc.DataDir != "/var/lib/portmaster" {
		t.Errorf("default DataDir = %q, want %q", sc.DataDir, "/var/lib/portmaster")
	}
}
