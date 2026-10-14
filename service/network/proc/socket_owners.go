//go:build linux

package proc

import (
	"slices"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/safing/portmaster/service/network/socket"
)

const (
	// socketOwnerGeneration defines how long the found owner of a socket is
	// reused without a new scan. Owners are kept for one to two generations,
	// so every socket in use is verified by a full scan at least every two
	// generations.
	socketOwnerGeneration = 1 * time.Second

	// socketOwnerMaxEntries defines how many owners the current generation may
	// hold before an early rotation is forced. This bounds memory usage.
	socketOwnerMaxEntries = 20000
)

// socketOwner is the result of a successful scan for a socket.
type socketOwner struct {
	uid int
	pid int
}

// socketOwners caches socket owners by socket inode. The socket tables are
// re-read up to 100 times per second, and every re-read creates new entries
// without a PID. Without this cache, every connection of a socket would
// trigger a full scan of /proc.
var socketOwners = struct {
	lock      sync.Mutex
	cur       map[int]socketOwner // [inode] = socketOwner
	prev      map[int]socketOwner // [inode] = socketOwner
	rotatedAt time.Time
}{
	cur:       make(map[int]socketOwner),
	prev:      make(map[int]socketOwner),
	rotatedAt: time.Now(),
}

// socketScans runs at most one scan of /proc per socket at a time, keyed by
// the socket name ("socket:[inode]"). A scan takes long when processes with
// many descriptors are running, and under a burst of new connections hundreds
// of lookups for the same socket start before the first scan finishes.
// Without this, each of them would walk /proc on its own and the cache would
// never be hit.
var socketScans singleflight.Group

// getCachedSocketOwner returns the cached owner of the socket if it still runs
// as the same user. pids must be the current PIDs of uid.
func getCachedSocketOwner(uid, inode int, pids []int) (pid int, ok bool) {
	socketOwners.lock.Lock()
	defer socketOwners.lock.Unlock()

	rotateSocketOwners()

	owner, ok := socketOwners.cur[inode]
	if !ok {
		// Do not promote to the current generation, so that the owner is
		// verified by a full scan at least every two generations.
		owner, ok = socketOwners.prev[inode]
	}
	if !ok {
		return socket.UndefinedProcessID, false
	}

	// The owner exited, or the inode belongs to another user's socket now.
	if owner.uid != uid || !slices.Contains(pids, owner.pid) {
		delete(socketOwners.cur, inode)
		delete(socketOwners.prev, inode)
		return socket.UndefinedProcessID, false
	}

	return owner.pid, true
}

// cacheSocketOwner records the owner of the socket found by a scan.
func cacheSocketOwner(uid, inode, pid int) {
	socketOwners.lock.Lock()
	defer socketOwners.lock.Unlock()

	rotateSocketOwners()
	if len(socketOwners.cur) >= socketOwnerMaxEntries {
		socketOwners.prev = socketOwners.cur
		socketOwners.cur = make(map[int]socketOwner)
		socketOwners.rotatedAt = time.Now()
	}
	socketOwners.cur[inode] = socketOwner{uid: uid, pid: pid}
}

// rotateSocketOwners drops generations that are due.
// The caller must hold socketOwners.lock.
func rotateSocketOwners() {
	switch since := time.Since(socketOwners.rotatedAt); {
	case since >= 2*socketOwnerGeneration:
		// No access for two generations: both are too old.
		socketOwners.prev = make(map[int]socketOwner)
		socketOwners.cur = make(map[int]socketOwner)
	case since >= socketOwnerGeneration:
		socketOwners.prev = socketOwners.cur
		socketOwners.cur = make(map[int]socketOwner)
	default:
		return
	}
	socketOwners.rotatedAt = time.Now()
}
