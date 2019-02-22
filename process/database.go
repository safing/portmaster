package process

import (
	"fmt"
	"sync"
	"time"

	"github.com/Safing/portbase/database"
	"github.com/Safing/portmaster/profile"
	"github.com/tevino/abool"
)

var (
	processes     = make(map[int]*Process)
	processesLock sync.RWMutex

	dbController     *database.Controller
	dbControllerFlag = abool.NewBool(false)
)

// GetProcessFromStorage returns a process from the internal storage.
func GetProcessFromStorage(pid int) (*Process, bool) {
	processesLock.RLock()
	defer processesLock.RUnlock()

	p, ok := processes[pid]
	return p, ok
}

// All returns a copy of all process objects.
func All() []*Process {
	processesLock.RLock()
	defer processesLock.RUnlock()

	all := make([]*Process, 0, len(processes))
	for _, proc := range processes {
		all = append(all, proc)
	}

	return all
}

// Save saves the process to the internal state and pushes an update.
func (p *Process) Save() {
	p.Lock()
	defer p.Unlock()

	if !p.KeyIsSet() {
		p.SetKey(fmt.Sprintf("network:tree/%d", p.Pid))
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

	if dbControllerFlag.IsSet() {
		go dbController.PushUpdate(p)
	}
}

// Delete deletes a process from the storage and propagates the change.
func (p *Process) Delete() {
	p.Lock()
	defer p.Unlock()

	processesLock.Lock()
	delete(processes, p.Pid)
	processesLock.Unlock()

	p.Meta().Delete()
	if dbControllerFlag.IsSet() {
		go dbController.PushUpdate(p)
	}

	// TODO: this should not be necessary, as processes should always have a profileSet.
	if p.profileSet != nil {
		profile.DeactivateProfileSet(p.profileSet)
	}
}

// CleanProcessStorage cleans the storage from old processes.
func CleanProcessStorage(thresholdDuration time.Duration) {
	processesLock.Lock()
	defer processesLock.Unlock()

	threshold := time.Now().Add(-thresholdDuration).Unix()
	for _, p := range processes {
		p.Lock()
		if p.FirstCommEstablished < threshold && p.CommCount == 0 {
			go p.Delete()
		}
		p.Unlock()
	}
}

// SetDBController sets the database controller and allows the package to push database updates on a save. It must be set by the package that registers the "network" database.
func SetDBController(controller *database.Controller) {
	dbController = controller
	dbControllerFlag.Set()
}
