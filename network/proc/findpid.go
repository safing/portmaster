// +build linux

package proc

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"sync"
	"syscall"

	"github.com/safing/portmaster/network/socket"

	"github.com/safing/portbase/log"
)

var (
	// pidsByUserLock is also used for locking the socketInfo.PID on all socket.*Info structs.
	pidsByUserLock sync.Mutex
	pidsByUser     = make(map[int][]int)
)

// FindConnectionPID returns the pid of the given socket info.
func FindConnectionPID(socketInfo *socket.ConnectionInfo) (pid int) {
	pidsByUserLock.Lock()
	defer pidsByUserLock.Unlock()

	if socketInfo.PID != socket.UnidentifiedProcessID {
		return socket.UnidentifiedProcessID
	}

	pid = findPID(socketInfo.UID, socketInfo.Inode)
	socketInfo.PID = pid
	return pid
}

// FindBindPID returns the pid of the given socket info.
func FindBindPID(socketInfo *socket.BindInfo) (pid int) {
	pidsByUserLock.Lock()
	defer pidsByUserLock.Unlock()

	if socketInfo.PID != socket.UnidentifiedProcessID {
		return socket.UnidentifiedProcessID
	}

	pid = findPID(socketInfo.UID, socketInfo.Inode)
	socketInfo.PID = pid
	return pid
}

// findPID returns the pid of the given uid and socket inode.
func findPID(uid, inode int) (pid int) { //nolint:gocognit // TODO

	pidsUpdated := false

	// get pids of user, update if missing
	pids, ok := pidsByUser[uid]
	if !ok {
		// log.Trace("proc: no processes of user, updating table")
		updatePids()
		pidsUpdated = true
		pids, ok = pidsByUser[uid]
	}
	if ok {
		// if user has pids, start checking them first
		var checkedUserPids []int
		for _, possiblePID := range pids {
			if findSocketFromPid(possiblePID, inode) {
				return possiblePID
			}
			checkedUserPids = append(checkedUserPids, possiblePID)
		}
		// if we fail on the first run and have not updated, update and check the ones we haven't tried so far.
		if !pidsUpdated {
			// log.Trace("proc: socket not found in any process of user, updating table")
			// update
			updatePids()
			// sort for faster search
			for i, j := 0, len(checkedUserPids)-1; i < j; i, j = i+1, j-1 {
				checkedUserPids[i], checkedUserPids[j] = checkedUserPids[j], checkedUserPids[i]
			}
			len := len(checkedUserPids)
			// check unchecked pids
			for _, possiblePID := range pids {
				// only check if not already checked
				if sort.SearchInts(checkedUserPids, possiblePID) == len {
					if findSocketFromPid(possiblePID, inode) {
						return possiblePID
					}
				}
			}
		}
	}

	// check all other pids
	// log.Trace("proc: socket not found in any process of user, checking all pids")
	// TODO: find best order for pidsByUser for best performance
	for possibleUID, pids := range pidsByUser {
		if possibleUID != uid {
			for _, possiblePID := range pids {
				if findSocketFromPid(possiblePID, inode) {
					return possiblePID
				}
			}
		}
	}

	return socket.UnidentifiedProcessID
}

func findSocketFromPid(pid, inode int) bool {
	socketName := fmt.Sprintf("socket:[%d]", inode)
	entries := readDirNames(fmt.Sprintf("/proc/%d/fd", pid))
	if len(entries) == 0 {
		return false
	}

	for _, entry := range entries {
		link, err := os.Readlink(fmt.Sprintf("/proc/%d/fd/%s", pid, entry))
		if err != nil {
			if !os.IsNotExist(err) {
				log.Warningf("proc: failed to read link /proc/%d/fd/%s: %s", pid, entry, err)
			}
			continue
		}
		if link == socketName {
			return true
		}
	}

	return false
}

func updatePids() {
	pidsByUser = make(map[int][]int)

	entries := readDirNames("/proc")
	if len(entries) == 0 {
		return
	}

entryLoop:
	for _, entry := range entries {
		pid, err := strconv.ParseInt(entry, 10, 32)
		if err != nil {
			continue entryLoop
		}

		statData, err := os.Stat(fmt.Sprintf("/proc/%d", pid))
		if err != nil {
			log.Warningf("proc: could not stat /proc/%d: %s", pid, err)
			continue entryLoop
		}
		sys, ok := statData.Sys().(*syscall.Stat_t)
		if !ok {
			log.Warningf("proc: unable to parse /proc/%d: wrong type", pid)
			continue entryLoop
		}

		pids, ok := pidsByUser[int(sys.Uid)]
		if ok {
			pidsByUser[int(sys.Uid)] = append(pids, int(pid))
		} else {
			pidsByUser[int(sys.Uid)] = []int{int(pid)}
		}

	}

	for _, slice := range pidsByUser {
		for i, j := 0, len(slice)-1; i < j; i, j = i+1, j-1 {
			slice[i], slice[j] = slice[j], slice[i]
		}
	}

}

func readDirNames(dir string) (names []string) {
	file, err := os.Open(dir)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Warningf("proc: could not open directory %s: %s", dir, err)
		}
		return
	}
	defer file.Close()
	names, err = file.Readdirnames(0)
	if err != nil {
		log.Warningf("proc: could not get entries from directory %s: %s", dir, err)
		return []string{}
	}
	return
}
