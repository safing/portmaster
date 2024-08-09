package main

import (
	"sync"

	"github.com/safing/portmaster/base/api/client"
	"github.com/safing/portmaster/base/log"
	"github.com/safing/structures/dsd"
)

const (
	subsystemsKeySpace = "runtime:subsystems/"

	// Module Failure Status Values
	// FailureNone    = 0 // unused
	// FailureHint    = 1 // unused.
	FailureWarning = 2
	FailureError   = 3
)

var (
	subsystems     = make(map[string]*Subsystem)
	subsystemsLock sync.Mutex
)

// Subsystem describes a subset of modules that represent a part of a
// service or program to the user. Subsystems can be (de-)activated causing
// all related modules to be brought down or up.
type Subsystem struct { //nolint:maligned // not worth the effort
	// ID is a unique identifier for the subsystem.
	ID string

	// Name holds a human readable name of the subsystem.
	Name string

	// Description may holds an optional description of
	// the subsystem's purpose.
	Description string

	// Modules contains all modules that are related to the subsystem.
	// Note that this slice also contains a reference to the subsystem
	// module itself.
	Modules []*ModuleStatus

	// FailureStatus is the worst failure status that is currently
	// set in one of the subsystem's dependencies.
	FailureStatus uint8
}

// ModuleStatus describes the status of a module.
type ModuleStatus struct {
	Name          string
	Enabled       bool
	Status        uint8
	FailureStatus uint8
	FailureID     string
	FailureMsg    string
}

// GetFailure returns the worst of all subsystem failures.
func GetFailure() (failureStatus uint8, failureMsg string) {
	subsystemsLock.Lock()
	defer subsystemsLock.Unlock()

	for _, subsystem := range subsystems {
		for _, module := range subsystem.Modules {
			if failureStatus < module.FailureStatus {
				failureStatus = module.FailureStatus
				failureMsg = module.FailureMsg
			}
		}
	}

	return
}

func updateSubsystem(s *Subsystem) {
	subsystemsLock.Lock()
	defer subsystemsLock.Unlock()

	subsystems[s.ID] = s
}

func clearSubsystems() {
	subsystemsLock.Lock()
	defer subsystemsLock.Unlock()

	for key := range subsystems {
		delete(subsystems, key)
	}
}

func subsystemsClient() {
	subsystemsOp := apiClient.Qsub("query "+subsystemsKeySpace, handleSubsystem)
	subsystemsOp.EnableResuscitation()
}

func handleSubsystem(m *client.Message) {
	switch m.Type {
	case client.MsgError:
	case client.MsgDone:
	case client.MsgSuccess:
	case client.MsgOk, client.MsgUpdate, client.MsgNew:

		newSubsystem := &Subsystem{}
		_, err := dsd.Load(m.RawValue, newSubsystem)
		if err != nil {
			log.Warningf("subsystems: failed to parse new subsystem: %s", err)
			return
		}
		updateSubsystem(newSubsystem)
		triggerTrayUpdate()

	case client.MsgDelete:
	case client.MsgWarning:
	case client.MsgOffline:

		clearSubsystems()

	}
}
