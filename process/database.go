package process

import (
	"fmt"
	"sync"

	"github.com/Safing/portbase/database"
	"github.com/tevino/abool"
)

var (
	processes     = make(map[int]*Process)
	processesLock sync.RWMutex

	dbController     *database.Controller
	dbControllerFlag = abool.NewBool(false)
)

func makeProcessKey(pid int) string {
	return fmt.Sprintf("network:tree/%d", pid)
}

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

	if p.DatabaseKey() == "" {
		p.SetKey(makeProcessKey(p.Pid))
		p.CreateMeta()

		processesLock.Lock()
		defer processesLock.Unlock()

		processes[p.Pid] = p
	}

	if dbControllerFlag.IsSet() {
		dbController.PushUpdate(p)
	}
}

// SetDBController sets the database controller and allows the package to push database updates on a save. It must be set by the package that registers the "network" database.
func SetDBController(controller *database.Controller) {
	dbController = controller
	dbControllerFlag.Set()
}
