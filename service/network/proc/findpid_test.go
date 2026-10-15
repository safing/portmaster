//go:build linux

package proc

import (
	"net"
	"os"
	"sync"
	"syscall"
	"testing"
)

// TestFindPIDSharedSocket looks up the owner of a socket held by the test
// process itself, from many goroutines at once. This exercises the real /proc
// walk, the single-flight scan and the owner cache end to end.
func TestFindPIDSharedSocket(t *testing.T) { //nolint:paralleltest // Uses the package-level cache.
	resetSocketOwners()

	// Open a UDP socket owned by this process and get its inode.
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatalf("failed to open udp socket: %s", err)
	}
	defer func() { _ = conn.Close() }()

	file, err := conn.File()
	if err != nil {
		t.Fatalf("failed to get socket file: %s", err)
	}
	defer func() { _ = file.Close() }()

	var stat syscall.Stat_t
	if err := syscall.Fstat(int(file.Fd()), &stat); err != nil {
		t.Fatalf("failed to stat socket: %s", err)
	}
	uid := os.Getuid()
	inode := int(stat.Ino) //nolint:gosec // Inode numbers fit into an int.
	want := os.Getpid()

	// Look the socket up from many goroutines at the same time, like a burst
	// of new connections on one socket does.
	const lookups = 32
	results := make([]int, lookups)
	var wg sync.WaitGroup
	for i := range lookups {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i] = findPID(uid, inode)
		}()
	}
	wg.Wait()

	for i, pid := range results {
		if pid != want {
			t.Fatalf("lookup %d returned pid %d, expected own pid %d", i, pid, want)
		}
	}

	// The scan result must now be in the cache.
	pids, ok := getPidsByUser(uid)
	if !ok {
		t.Fatalf("no pid list for uid %d", uid)
	}
	if pid, ok := getCachedSocketOwner(uid, inode, pids); !ok || pid != want {
		t.Fatalf("expected cached owner %d, got %d (ok=%v)", want, pid, ok)
	}
}
