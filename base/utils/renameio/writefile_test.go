//go:build darwin || dragonfly || freebsd || linux || nacl || netbsd || openbsd || solaris || windows

package renameio

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteFile(t *testing.T) {
	t.Parallel()

	d, err := os.MkdirTemp("", "test-renameio-testwritefile")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = os.RemoveAll(d)
	}()

	filename := filepath.Join(d, "hello.sh")

	wantData := []byte("#!/bin/sh\necho \"Hello World\"\n")
	wantPerm := os.FileMode(0o0600)
	if err := WriteFile(filename, wantData, wantPerm); err != nil {
		t.Fatal(err)
	}

	gotData, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(gotData, wantData) {
		t.Errorf("got data %v, want data %v", gotData, wantData)
	}

	fi, err := os.Stat(filename)
	if err != nil {
		t.Fatal(err)
	}
	if gotPerm := fi.Mode() & os.ModePerm; gotPerm != wantPerm {
		t.Errorf("got permissions 0%o, want permissions 0%o", gotPerm, wantPerm)
	}
}
