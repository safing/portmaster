package process

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	processInfo "github.com/shirou/gopsutil/process"
	"github.com/tevino/abool"

	"github.com/safing/portmaster/base/database"
	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/service/profile"
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

// GetProcessesWithProfile returns all processes that use the given profile.
// If preferProcessGroupLeader is set, it returns the process group leader instead, if available.
func GetProcessesWithProfile(ctx context.Context, profileSource profile.ProfileSource, profileID string, preferProcessGroupLeader bool) []*Process {
	log.Tracer(ctx).Debugf("process: searching for processes belonging to %s", profile.MakeScopedID(profileSource, profileID))

	// Get all processes that match the given profile.
	procs := make([]*Process, 0, 8)
	for _, p := range All() {
		lp := p.profile.LocalProfile()
		if lp != nil && lp.Source == profileSource && lp.ID == profileID {
			if preferProcessGroupLeader && p.Leader() != nil {
				procs = append(procs, p.Leader())
			} else {
				procs = append(procs, p)
			}
		}
	}

	// Sort and compact.
	slices.SortFunc[[]*Process, *Process](procs, func(a, b *Process) int {
		return strings.Compare(a.processKey, b.processKey)
	})

	procs = slices.CompactFunc[[]*Process, *Process](procs, func(a, b *Process) bool {
		return a.processKey == b.processKey
	})

	return procs
}

// Save saves the process to the internal state and pushes an update.
func (p *Process) Save() {
	p.Lock()
	defer p.Unlock()

	p.UpdateMeta()

	if p.processKey == "" {
		p.processKey = getProcessKey(int32(p.Pid), p.CreatedAt)
	}

	if !p.KeyIsSet() {
		// set key
		p.SetKey(fmt.Sprintf("%s/%s", processDatabaseNamespace, p.processKey))

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
		switch p.Pid {
		case UnidentifiedProcessID, UnsolicitedProcessID, SystemProcessID:
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
