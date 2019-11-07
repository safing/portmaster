package process

import (
	"fmt"
	"sync"
	"time"

	processInfo "github.com/shirou/gopsutil/process"

	"github.com/safing/portbase/database"
	"github.com/safing/portbase/log"
	"github.com/safing/portmaster/profile"
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

	deleteProcessesThreshold = 15 * time.Minute
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

	if !p.KeyIsSet() {
		p.SetKey(fmt.Sprintf("%s/%d", processDatabaseNamespace, p.Pid))
		p.CreateMeta()
	}

	processesLock.RLock()
	_, ok := processes[p.Pid]
	processesLock.RUnlock()

	if !ok {
		processesLock.Lock()
		processes[p.Pid] = p
		processesLock.Unlock()
	}

	if dbControllerFlag.IsSet() && p.Error == "" {
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

	// deactivate profile
	// TODO: check if there is another process using the same profile set
	if p.profileSet != nil {
		profile.DeactivateProfileSet(p.profileSet)
	}
}

// CleanProcessStorage cleans the storage from old processes.
func CleanProcessStorage(activeComms map[string]struct{}) {
	activePIDs, err := getActivePIDs()
	if err != nil {
		log.Warningf("process: failed to get list of active PIDs: %s", err)
		activePIDs = nil
	}
	processesCopy := All()

	threshold := time.Now().Add(-deleteProcessesThreshold).Unix()
	delete := false

	// clean primary processes
	for _, p := range processesCopy {
		p.Lock()
		// check if internal
		if p.Pid <= 0 {
			p.Unlock()
			continue
		}

		// has comms?
		_, hasComms := activeComms[p.DatabaseKey()]

		// virtual / active
		virtual := p.Virtual
		active := false
		if activePIDs != nil {
			_, active = activePIDs[p.Pid]
		}
		p.Unlock()

		if !virtual && !hasComms && !active && p.LastCommEstablished < threshold {
			go p.Delete()
		}
	}

	// clean virtual/failed processes
	for _, p := range processesCopy {
		p.Lock()
		// check if internal
		if p.Pid <= 0 {
			p.Unlock()
			continue
		}

		switch {
		case p.Error != "":
			if p.Meta().Created < threshold {
				delete = true
			}
		case p.Virtual:
			_, parentIsActive := processesCopy[p.ParentPid]
			active := true
			if activePIDs != nil {
				_, active = activePIDs[p.Pid]
			}
			if !parentIsActive || !active {
				delete = true
			}
		}
		p.Unlock()

		if delete {
			log.Tracef("process.clean: deleted %s", p.DatabaseKey())
			go p.Delete()
			delete = false
		}
	}
}

func getActivePIDs() (map[int]struct{}, error) {
	procs, err := processInfo.Processes()
	if err != nil {
		return nil, err
	}

	activePIDs := make(map[int]struct{})
	for _, p := range procs {
		activePIDs[int(p.Pid)] = struct{}{}
	}

	return activePIDs, nil
}

// SetDBController sets the database controller and allows the package to push database updates on a save. It must be set by the package that registers the "network" database.
func SetDBController(controller *database.Controller) {
	dbController = controller
	dbControllerFlag.Set()
}
