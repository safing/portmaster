package process

import (
	"fmt"
	"sync"
	"time"

	processInfo "github.com/shirou/gopsutil/process"
	"github.com/tevino/abool"

	"github.com/safing/portbase/database"
	"github.com/safing/portbase/log"
)

const processDatabaseNamespace = "network:tree"

var (
	processes     = make(map[string]*Process)
	processesLock sync.RWMutex

	dbController     *database.Controller
	dbControllerFlag = abool.NewBool(false)

	deleteProcessesThreshold = 7 * time.Minute
)

// GetProcessFromStorage returns a process from the internal storage.
func GetProcessFromStorage(key string) (*Process, bool) {
	processesLock.RLock()
	defer processesLock.RUnlock()

	p, ok := processes[key]
	return p, ok
}

// All returns a copy of all process objects.
func All() map[int]*Process {
	processesLock.RLock()
	defer processesLock.RUnlock()

	all := make(map[int]*Process)
	for _, proc := range processes {
		all[proc.Pid] = proc
	}

	return all
}

// Save saves the process to the internal state and pushes an update.
func (p *Process) Save() {
	p.Lock()
	defer p.Unlock()

	p.UpdateMeta()

	if !p.KeyIsSet() {
		// set key
		p.SetKey(fmt.Sprintf("%s/%s", processDatabaseNamespace, getProcessKey(int32(p.Pid), p.CreatedAt)))

		// save
		processesLock.Lock()
		processes[p.processKey] = p
		processesLock.Unlock()
	}

	if dbControllerFlag.IsSet() {
		dbController.PushUpdate(p)
	}
}

// Delete deletes a process from the storage and propagates the change.
func (p *Process) Delete() {
	p.Lock()
	defer p.Unlock()

	// delete from internal storage
	processesLock.Lock()
	delete(processes, p.processKey)
	processesLock.Unlock()

	// propagate delete
	p.Meta().Delete()
	if dbControllerFlag.IsSet() {
		dbController.PushUpdate(p)
	}

	// TODO: maybe mark the assigned profiles as no longer needed?
}

// CleanProcessStorage cleans the storage from old processes.
func CleanProcessStorage(activePIDs map[int]struct{}) {
	// add system table of processes
	pids, err := processInfo.Pids()
	if err != nil {
		log.Warningf("process: failed to get list of active PIDs: %s", err)
	} else {
		for _, pid := range pids {
			activePIDs[int(pid)] = struct{}{}
		}
	}

	processesCopy := All()
	threshold := time.Now().Add(-deleteProcessesThreshold).Unix()

	// clean primary processes
	for _, p := range processesCopy {
		// The PID of a process does not change.

		// Check if this is a special process.
		if p.Pid == UnidentifiedProcessID || p.Pid == SystemProcessID {
			p.profile.MarkStillActive()
			continue
		}

		// Check if process is active.
		_, active := activePIDs[p.Pid]
		if active {
			p.profile.MarkStillActive()
			continue
		}

		// Process is inactive, start deletion process
		lastSeen := p.GetLastSeen()
		switch {
		case lastSeen == 0:
			// add last seen timestamp
			p.SetLastSeen(time.Now().Unix())
		case lastSeen > threshold:
			// within keep period
		default:
			// delete now
			p.Delete()
			log.Tracef("process: cleaned %s", p.DatabaseKey())
		}
	}
}

// SetDBController sets the database controller and allows the package to push database updates on a save. It must be set by the package that registers the "network" database.
func SetDBController(controller *database.Controller) {
	dbController = controller
	dbControllerFlag.Set()
}
