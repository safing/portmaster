//go:build linux

package proc

import (
	"testing"
	"time"
)

// The tests share the package-level cache, so they must not run in parallel.

func resetSocketOwners() {
	socketOwners.lock.Lock()
	defer socketOwners.lock.Unlock()

	socketOwners.cur = make(map[int]socketOwner)
	socketOwners.prev = make(map[int]socketOwner)
	socketOwners.rotatedAt = time.Now()
}

func setSocketOwnersRotatedAt(t time.Time) {
	socketOwners.lock.Lock()
	defer socketOwners.lock.Unlock()

	socketOwners.rotatedAt = t
}

func TestSocketOwnerCache(t *testing.T) { //nolint:paralleltest // Uses the package-level cache.
	resetSocketOwners()
	cacheSocketOwner(1000, 42, 500)

	if pid, ok := getCachedSocketOwner(1000, 42, []int{100, 500}); !ok || pid != 500 {
		t.Fatalf("expected cached owner 500, got %d (ok=%v)", pid, ok)
	}
	if _, ok := getCachedSocketOwner(1001, 42, []int{500}); ok {
		t.Fatal("owner of another user must not be returned")
	}

	cacheSocketOwner(1000, 43, 600)
	if _, ok := getCachedSocketOwner(1000, 43, []int{100}); ok {
		t.Fatal("owner that is no longer running must not be returned")
	}
	if _, ok := getCachedSocketOwner(1000, 43, []int{100, 600}); ok {
		t.Fatal("invalid owner must be removed from the cache")
	}
}

func TestSocketOwnerCacheExpiry(t *testing.T) { //nolint:paralleltest // Uses the package-level cache.
	resetSocketOwners()
	cacheSocketOwner(1000, 42, 500)

	setSocketOwnersRotatedAt(time.Now().Add(-socketOwnerGeneration))
	if _, ok := getCachedSocketOwner(1000, 42, []int{500}); !ok {
		t.Fatal("owner must survive one rotation")
	}

	setSocketOwnersRotatedAt(time.Now().Add(-socketOwnerGeneration))
	if _, ok := getCachedSocketOwner(1000, 42, []int{500}); ok {
		t.Fatal("owner must expire after two rotations")
	}
}

func TestSocketOwnerCacheIdleExpiry(t *testing.T) { //nolint:paralleltest // Uses the package-level cache.
	resetSocketOwners()
	cacheSocketOwner(1000, 42, 500)

	setSocketOwnersRotatedAt(time.Now().Add(-2 * socketOwnerGeneration))
	if _, ok := getCachedSocketOwner(1000, 42, []int{500}); ok {
		t.Fatal("owner must expire after an idle period of two generations")
	}
}

func TestSocketOwnerCacheMaxEntries(t *testing.T) { //nolint:paralleltest // Uses the package-level cache.
	resetSocketOwners()
	for inode := range socketOwnerMaxEntries {
		cacheSocketOwner(1000, inode, 500)
	}
	// The current generation is full, so this forces an early rotation.
	cacheSocketOwner(1000, socketOwnerMaxEntries, 500)

	socketOwners.lock.Lock()
	curLen, prevLen := len(socketOwners.cur), len(socketOwners.prev)
	socketOwners.lock.Unlock()
	if curLen != 1 || prevLen != socketOwnerMaxEntries {
		t.Fatalf("expected early rotation (cur=1, prev=%d), got cur=%d, prev=%d", socketOwnerMaxEntries, curLen, prevLen)
	}
}
