//go:build linux

package renameio

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestTempDir(t *testing.T) {
	t.Parallel()

	if tmpdir, ok := os.LookupEnv("TMPDIR"); ok {
		t.Cleanup(func() {
			_ = os.Setenv("TMPDIR", tmpdir) // restore
		})
	} else {
		t.Cleanup(func() {
			_ = os.Unsetenv("TMPDIR") // restore
		})
	}

	mount1, err := os.MkdirTemp("", "test-renameio-testtempdir1")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(mount1)
	})

	mount2, err := os.MkdirTemp("", "test-renameio-testtempdir2")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(mount2)
	})

	if err := syscall.Mount("tmpfs", mount1, "tmpfs", 0, ""); err != nil {
		t.Skipf("cannot mount tmpfs on %s: %v", mount1, err)
	}
	t.Cleanup(func() {
		_ = syscall.Unmount(mount1, 0)
	})

	if err := syscall.Mount("tmpfs", mount2, "tmpfs", 0, ""); err != nil {
		t.Skipf("cannot mount tmpfs on %s: %v", mount2, err)
	}
	t.Cleanup(func() {
		_ = syscall.Unmount(mount2, 0)
	})

	tests := []struct {
		name   string
		dir    string
		path   string
		TMPDIR string
		want   string
	}{
		{
			name: "implicit TMPDIR",
			path: filepath.Join(os.TempDir(), "foo.txt"),
			want: os.TempDir(),
		},

		{
			name:   "explicit TMPDIR",
			path:   filepath.Join(mount1, "foo.txt"),
			TMPDIR: mount1,
			want:   mount1,
		},

		{
			name:   "explicit unsuitable TMPDIR",
			path:   filepath.Join(mount1, "foo.txt"),
			TMPDIR: mount2,
			want:   mount1,
		},

		{
			name:   "nonexistant TMPDIR",
			path:   filepath.Join(mount1, "foo.txt"),
			TMPDIR: "/nonexistant",
			want:   mount1,
		},

		{
			name:   "caller-specified",
			dir:    "/overridden",
			path:   filepath.Join(mount1, "foo.txt"),
			TMPDIR: "/nonexistant",
			want:   "/overridden",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if testCase.TMPDIR == "" {
				_ = os.Unsetenv("TMPDIR")
			} else {
				_ = os.Setenv("TMPDIR", testCase.TMPDIR)
			}

			if got := tempDir(testCase.dir, testCase.path); got != testCase.want {
				t.Fatalf("tempDir(%q, %q): got %q, want %q", testCase.dir, testCase.path, got, testCase.want)
			}
		})
	}
}
