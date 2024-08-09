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

	// Look through the PIDs in reverse order, because higher/newer PIDs will be more likely to
	// be searched for.
	for j := len(pids) - 1; j >= 0; j-- {
		if pidHasSocket(pids[j], socketName) {
			return pids[j]
		}
	}

	return socket.UndefinedProcessID
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
