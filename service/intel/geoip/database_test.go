package geoip

import (
	"os"
	"path/filepath"
	"testing"
)

// TestFileReplaced checks the detection of a replaced database file. The
// replacement case runs first, on an identity not compared before, with the
// replacement carrying the same size and mtime, so that only the file ID
// distinguishes the files. This is what catches a lazily resolved file ID.
func TestFileReplaced(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	active := filepath.Join(dir, v4MMDBResource)
	purged := filepath.Join(dir, "purged.mmdb")
	downloaded := filepath.Join(dir, "downloaded.mmdb")

	content := []byte("mapped database")
	if err := os.WriteFile(active, content, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(downloaded, content, 0o600); err != nil {
		t.Fatal(err)
	}

	// Capture the identity as getGeoIPDB does, before anything is compared.
	mapped, err := fileIdentity(active)
	if err != nil {
		t.Fatal(err)
	}

	// Give the replacement the same timestamp as the mapped file.
	if err := os.Chtimes(downloaded, mapped.ModTime(), mapped.ModTime()); err != nil {
		t.Fatal(err)
	}

	// Updater: move the mapped file away, rename the replacement into place.
	if err := os.Rename(active, purged); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(downloaded, active); err != nil {
		t.Fatal(err)
	}

	// First comparison only now: the file at the path must be seen as replaced.
	replaced, err := fileReplaced(mapped, active)
	if err != nil {
		t.Fatal(err)
	}
	if !replaced {
		t.Error("file renamed into the artifact path not reported as replaced")
	}

	// The moved file is still the mapped file.
	replaced, err = fileReplaced(mapped, purged)
	if err != nil {
		t.Fatal(err)
	}
	if replaced {
		t.Error("moved file reported as replaced")
	}

	// A separately captured identity of an untouched file: no reload.
	untouched, err := fileIdentity(active)
	if err != nil {
		t.Fatal(err)
	}
	replaced, err = fileReplaced(untouched, active)
	if err != nil {
		t.Fatal(err)
	}
	if replaced {
		t.Error("untouched file reported as replaced")
	}

	// Missing file: an error, not a verdict.
	if _, err := fileReplaced(mapped, filepath.Join(dir, "missing.mmdb")); err == nil {
		t.Error("missing file did not return an error")
	}
}
