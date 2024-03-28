package docks

import (
	"encoding/hex"
	"errors"
	"fmt"
	"sync"

	"github.com/safing/portbase/modules"
	"github.com/safing/portbase/rng"
	_ "github.com/safing/portmaster/spn/access"
)

var (
	module *modules.Module

	allCranes      = make(map[string]*Crane) // ID = Crane ID
	assignedCranes = make(map[string]*Crane) // ID = connected Hub ID
	cranesLock     sync.RWMutex

	runningTests bool
)

func init() {
	module = modules.Register("docks", nil, start, stopAllCranes, "terminal", "cabin", "access")
}

func start() error {
	return registerMetrics()
}

func registerCrane(crane *Crane) error {
	cranesLock.Lock()
	defer cranesLock.Unlock()

	// Generate new IDs until a unique one is found.
	for i := 0; i < 100; i++ {
		// Generate random ID.
		randomID, err := rng.Bytes(3)
		if err != nil {
			return fmt.Errorf("failed to generate crane ID: %w", err)
		}
		newID := hex.EncodeToString(randomID)

		// Check if ID already exists.
		_, ok := allCranes[newID]
		if !ok {
			crane.ID = newID
			allCranes[crane.ID] = crane
			return nil
		}
	}

	return errors.New("failed to find unique crane ID")
}

func unregisterCrane(crane *Crane) {
	cranesLock.Lock()
	defer cranesLock.Unlock()

	delete(allCranes, crane.ID)
	if crane.ConnectedHub != nil {
		delete(assignedCranes, crane.ConnectedHub.ID)
	}
}

func stopAllCranes() error {
	for _, crane := range getAllCranes() {
		crane.Stop(nil)
	}
	return nil
}

// AssignCrane assigns a crane to the given Hub ID.
func AssignCrane(hubID string, crane *Crane) {
	cranesLock.Lock()
	defer cranesLock.Unlock()

	assignedCranes[hubID] = crane
}

// GetAssignedCrane returns the assigned crane of the given Hub ID.
func GetAssignedCrane(hubID string) *Crane {
	cranesLock.RLock()
	defer cranesLock.RUnlock()

	crane, ok := assignedCranes[hubID]
	if ok {
		return crane
	}
	return nil
}

func getAllCranes() map[string]*Crane {
	copiedCranes := make(map[string]*Crane, len(allCranes))

	cranesLock.RLock()
	defer cranesLock.RUnlock()

	for id, crane := range allCranes {
		copiedCranes[id] = crane
	}
	return copiedCranes
}

// GetAllAssignedCranes returns a copy of the map of all assigned cranes.
func GetAllAssignedCranes() map[string]*Crane {
	copiedCranes := make(map[string]*Crane, len(assignedCranes))

	cranesLock.RLock()
	defer cranesLock.RUnlock()

	for destination, crane := range assignedCranes {
		copiedCranes[destination] = crane
	}
	return copiedCranes
}
