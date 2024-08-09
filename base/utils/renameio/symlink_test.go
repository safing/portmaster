//go:build darwin || dragonfly || freebsd || linux || nacl || netbsd || openbsd || solaris || windows

package renameio

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestSymlink(t *testing.T) {
	t.Parallel()

	d, err := os.MkdirTemp("", "test-renameio-testsymlink")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(d)
	})

	want := []byte("Hello World")
	if err := os.WriteFile(filepath.Join(d, "hello.txt"), want, 0o0600); err != nil {
		t.Fatal(err)
	}

	for range 2 {
		if err := Symlink("hello.txt", filepath.Join(d, "hi.txt")); err != nil {
			t.Fatal(err)
		}

		got, err := os.ReadFile(filepath.Join(d, "hi.txt"))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("unexpected content: got %q, want %q", string(got), string(want))
		}
	}
}
