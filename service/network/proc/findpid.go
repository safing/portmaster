//go:build linux

package proc

import (
	"errors"
	"io/fs"
	"os"
	"strconv"

	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/service/network/socket"
)

// GetPID returns the already existing pid of the given socket info or searches for it.
// This also acts as a getter for socket.Info.PID, as locking for that occurs here.
func GetPID(socketInfo socket.Info) (pid int) {
	// Get currently assigned PID to the socket info.
	currentPid := socketInfo.GetPID()

	// If the current PID already is valid (ie. not unidentified), return it immediately.
	if currentPid != socket.UndefinedProcessID {
		return currentPid
	}

	// Find PID for the given UID and inode.
	pid = findPID(socketInfo.GetUIDandInode())

	// Set the newly found PID on the socket info.
	socketInfo.SetPID(pid)

	// Return found PID.
	return pid
}

// findPID returns the pid of the given uid and socket inode.
func findPID(uid, inode int) (pid int) {
	socketName := "socket:[" + strconv.Itoa(inode) + "]"

	// Always update pid table (it has a call limiter anyway)
	updatePids()

	// Get all pids for the given uid.
	pids, ok := getPidsByUser(uid)
	if !ok {
		return socket.UndefinedProcessID
	}

	// The cache and the single-flight below only help when the same socket is
	// looked up again and again. That happens when one socket carries many
	// connections, e.g. a torrent client talking to hundreds of peers through
	// one UDP socket, or a server accepting connections on its listening
	// socket. Every new connection of such a socket needs the owner, and the
	// socket table forgets the PID on every refresh. A socket with a single
	// connection is looked up once and simply scans.

	// Reuse the owner found by an earlier scan, if it is still running.
	if pid, ok := getCachedSocketOwner(uid, inode, pids); ok {
		return pid
	}

	// Run at most one scan per socket at a time. If another lookup is already
	// scanning for this socket, wait for its result instead of walking /proc
	// again. A failed scan is shared too; the caller retries anyway.
	result, _, _ := socketScans.Do(socketName, func() (any, error) {
		// Look through the PIDs in reverse order, because higher/newer PIDs will be more likely to
		// be searched for.
		for j := len(pids) - 1; j >= 0; j-- {
			if pidHasSocket(pids[j], socketName) {
				cacheSocketOwner(uid, inode, pids[j])
				return pids[j], nil
			}
		}
		return socket.UndefinedProcessID, nil
	})
	return result.(int) //nolint:forcetypeassert // Can only be an int.
}

func pidHasSocket(pid int, socketName string) bool {
	socketBase := "/proc/" + strconv.Itoa(pid) + "/fd"
	entries := readDirNames(socketBase)
	if len(entries) == 0 {
		return false
	}

	socketBase += "/"
	// Look through the FDs in reverse order, because higher/newer FDs will be
	// more likely to be searched for.
	for i := len(entries) - 1; i >= 0; i-- {
		link, err := os.Readlink(socketBase + entries[i])
		if err != nil {
			if !errors.Is(err, fs.ErrNotExist) {
				log.Warningf("proc: failed to read link /proc/%d/fd/%s: %s", pid, entries[i], err)
			}
			continue
		}
		if link == socketName {
			return true
		}
	}

	return false
}

// readDirNames only reads the directory names. Using os.ReadDir() would call `lstat` on every
// resulting directory name, which we don't need. This function will be called a lot, so we should
// refrain from unnecessary work.
func readDirNames(dir string) (names []string) {
	file, err := os.Open(dir)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			log.Warningf("proc: could not open directory %s: %s", dir, err)
		}
		return
	}
	defer func() {
		_ = file.Close()
	}()

	names, err = file.Readdirnames(0)
	if err != nil {
		log.Warningf("proc: could not get entries from directory %s: %s", dir, err)
		return []string{}
	}
	return
}
