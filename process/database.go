package process

import (
	"fmt"
	"sync"
	"time"

	processInfo "github.com/shirou/gopsutil/process"

	"github.com/safing/portbase/database"
	"github.com/safing/portbase/log"
	"github.com/tevino/abool"
)

const (
	processDatabaseNamespace = "network:tree"
)

var (
	processes     = make(map[int]*Process)
	processesLock sync.RWMutex

	dbController     *database.Controller
	dbControllerFlag = abool.NewBool(false)

	deleteProcessesThreshold = 7 * time.Minute
)

// GetProcessFromStorage returns a process from the internal storage.
func GetProcessFromStorage(pid int) (*Process, bool) {
	processesLock.RLock()
	defer processesLock.RUnlock()

	p, ok := processes[pid]
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
		p.SetKey(fmt.Sprintf("%s/%d", processDatabaseNamespace, p.Pid))

		// save
		processesLock.Lock()
		processes[p.Pid] = p
		processesLock.Unlock()
	}

	if dbControllerFlag.IsSet() {
		go dbController.PushUpdate(p)
	}
}

// Delete deletes a process from the storage and propagates the change.
func (p *Process) Delete() {
	p.Lock()
	defer p.Unlock()

	// delete from internal storage
	processesLock.Lock()
	delete(processes, p.Pid)
	processesLock.Unlock()

	// propagate delete
	p.Meta().Delete()
	if dbControllerFlag.IsSet() {
		go dbController.PushUpdate(p)
	}

	// TODO: maybe mark the assigned profiles as no longer needed?
}

// CleanProcessStorage cleans the storage from old processes.
func CleanProcessStorage(activePIDs map[int]struct{}) {
	// add system table of processes
	procs, err := processInfo.Processes()
	if err != nil {
		log.Warningf("process: failed to get list of active PIDs: %s", err)
	} else {
		for _, p := range procs {
			activePIDs[int(p.Pid)] = struct{}{}
		}
	}

	processesCopy := All()
	threshold := time.Now().Add(-deleteProcessesThreshold).Unix()

	// clean primary processes
	for _, p := range processesCopy {
		p.Lock()

		_, active := activePIDs[p.Pid]
		switch {
		case p.Pid == UnidentifiedProcessID:
			// internal
		case p.Pid == SystemProcessID:
			// internal
		case active:
			// process in system process table or recently seen on the network
		default:
			// delete now or soon
			switch {
			case p.LastSeen == 0:
				// add last
				p.LastSeen = time.Now().Unix()
			case p.LastSeen > threshold:
				// within keep period
			default:
				// delete now
				log.Tracef("process.clean: deleted %s", p.DatabaseKey())
				go p.Delete()
			}
		}

		p.Unlock()
	}
}

// SetDBController sets the database controller and allows the package to push database updates on a save. It must be set by the package that registers the "network" database.
func SetDBController(controller *database.Controller) {
	dbController = controller
	dbControllerFlag.Set()
}
