//go:build linux

package proc

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strconv"
	"sync"
	"syscall"

	"github.com/safing/portbase/log"
	"github.com/safing/portbase/utils"
)

var (
	// pidsByUserLock is also used for locking the socketInfo.PID on all socket.*Info structs.
	pidsByUser      = make(map[int][]int)
	pidsByUserLock  sync.RWMutex
	fetchPidsByUser utils.OnceAgain
)

// getPidsByUser returns the cached PIDs for the given UID.
func getPidsByUser(uid int) (pids []int, ok bool) {
	pidsByUserLock.RLock()
	defer pidsByUserLock.RUnlock()

	pids, ok = pidsByUser[uid]
	return
}

// updatePids fetches and creates a new pidsByUser map using utils.OnceAgain.
func updatePids() {
	fetchPidsByUser.Do(func() {
		newPidsByUser := make(map[int][]int)
		pidCnt := 0

		entries := readDirNames("/proc")
		if len(entries) == 0 {
			log.Warning("proc: found no PIDs in /proc")
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
				if !errors.Is(err, fs.ErrNotExist) {
					log.Warningf("proc: could not stat /proc/%d: %s", pid, err)
				}
				continue entryLoop
			}
			sys, ok := statData.Sys().(*syscall.Stat_t)
			if !ok {
				log.Warningf("proc: unable to parse /proc/%d: wrong type", pid)
				continue entryLoop
			}

			pids, ok := newPidsByUser[int(sys.Uid)]
			if ok {
				newPidsByUser[int(sys.Uid)] = append(pids, int(pid))
			} else {
				newPidsByUser[int(sys.Uid)] = []int{int(pid)}
			}
			pidCnt++
		}

		// log.Tracef("proc: updated PID table with %d entries", pidCnt)

		pidsByUserLock.Lock()
		defer pidsByUserLock.Unlock()
		pidsByUser = newPidsByUser
	})
}
