//go:build linux

package bbolt

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// TestNewBBoltClosesDBOnBucketCreationFailure checks that NewBBolt closes the
// opened database if creating the bucket fails. Otherwise the file lock of the
// leaked database blocks every later attempt to open the same database within
// this process.
//
// The bucket creation is made to fail by limiting the maximum file size of the
// process (RLIMIT_FSIZE) to the size of a freshly initialized database file:
// bbolt.Open succeeds, but committing the bucket needs to write beyond it and
// fails with EFBIG (Go ignores the accompanying SIGXFSZ).
// This test changes a process wide limit and must not run in parallel.
func TestNewBBoltClosesDBOnBucketCreationFailure(t *testing.T) { //nolint:paralleltest // Changes a process wide limit.
	dir := t.TempDir()
	pageSize := int64(os.Getpagesize())

	var orig syscall.Rlimit
	err := syscall.Getrlimit(syscall.RLIMIT_FSIZE, &orig)
	if err != nil {
		t.Skipf("cannot get file size limit: %s", err)
	}
	limited := orig
	// bbolt initializes a new database file with 4 pages.
	limited.Cur = uint64(4 * pageSize) //nolint:gosec // The page size is positive.
	if orig.Max != ^uint64(0) && orig.Max < limited.Cur {
		t.Skip("file size limit is too low")
	}
	err = syscall.Setrlimit(syscall.RLIMIT_FSIZE, &limited)
	if err != nil {
		t.Skipf("cannot set file size limit: %s", err)
	}
	_, err = NewBBolt("test", dir)
	rErr := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &orig)
	if rErr != nil {
		t.Fatalf("failed to restore file size limit: %s", rErr)
	}
	if err == nil {
		t.Skip("bucket creation did not fail with a limited file size")
	}
	t.Logf("bucket creation failed as intended: %s", err)
	fi, statErr := os.Stat(filepath.Join(dir, "db.bbolt"))
	if statErr != nil || fi.Size() == 0 {
		t.Fatalf("database file was not created by bbolt.Open: %v", statErr)
	}

	// Opening the database again must not be blocked by the lock of the
	// failed attempt. Without the fix, this fails after six open timeouts.
	start := time.Now()
	db, err := NewBBolt("test", dir)
	if err != nil {
		t.Fatalf("reopening the database after a failed open failed after %s: %s", time.Since(start), err)
	}
	err = db.Shutdown()
	if err != nil {
		t.Fatal(err)
	}
}
