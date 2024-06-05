package broadcasts

import (
	"fmt"
	"sync"
	"time"

	"github.com/safing/portmaster/base/database/record"
)

const broadcastStatesDBKey = "core:broadcasts/state"

// BroadcastStates holds states for broadcast notifications.
type BroadcastStates struct {
	record.Base
	sync.Mutex

	States map[string]*BroadcastState
}

// BroadcastState holds state for a single broadcast notifications.
type BroadcastState struct {
	Read time.Time
}

func (bss *BroadcastStates) save() error {
	return db.Put(bss)
}

// getbroadcastStates returns the broadcast states from the database.
func getBroadcastStates() (*BroadcastStates, error) {
	r, err := db.Get(broadcastStatesDBKey)
	if err != nil {
		return nil, err
	}

	// Unwrap.
	if r.IsWrapped() {
		// Only allocate a new struct, if we need it.
		newRecord := &BroadcastStates{}
		err = record.Unwrap(r, newRecord)
		if err != nil {
			return nil, err
		}
		return newRecord, nil
	}

	// or adjust type
	newRecord, ok := r.(*BroadcastStates)
	if !ok {
		return nil, fmt.Errorf("record not of type *BroadcastStates, but %T", r)
	}
	return newRecord, nil
}

// newBroadcastStates returns a new BroadcastStates.
func newBroadcastStates() *BroadcastStates {
	bss := &BroadcastStates{
		States: make(map[string]*BroadcastState),
	}
	bss.SetKey(broadcastStatesDBKey)

	return bss
}
