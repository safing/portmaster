package docks

import (
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/safing/portmaster/base/rng"
	"github.com/safing/portmaster/service/mgr"
	_ "github.com/safing/portmaster/spn/access"
)

// Docks handles connections to other network participants.
type Docks struct {
	mgr      *mgr.Manager
	instance instance
}

// Manager returns the module manager.
func (d *Docks) Manager() *mgr.Manager {
	return d.mgr
}

// Start starts the module.
func (d *Docks) Start() error {
	return start()
}

// Stop stops the module.
func (d *Docks) Stop() error {
	return stopAllCranes()
}

var (
	allCranes      = make(map[string]*Crane) // ID = Crane ID
	assignedCranes = make(map[string]*Crane) // ID = connected Hub ID
	cranesLock     sync.RWMutex

	runningTests bool
)

func start() error {
	return registerMetrics()
}

func registerCrane(crane *Crane) error {
	cranesLock.Lock()
	defer cranesLock.Unlock()

	// Generate new IDs until a unique one is found.
	for range 100 {
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

var (
	module     *Docks
	shimLoaded atomic.Bool
)

// New returns a new Docks module.
func New(instance instance) (*Docks, error) {
	if !shimLoaded.CompareAndSwap(false, true) {
		return nil, errors.New("only one instance allowed")
	}
	m := mgr.New("Docks")
	module = &Docks{
		mgr:      m,
		instance: instance,
	}
	return module, nil
}

type instance interface{}
