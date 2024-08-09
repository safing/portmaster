package database

import (
	"context"
	"time"
)

// Maintain runs the Maintain method on all storages.
func Maintain(ctx context.Context) (err error) {
	// copy, as we might use the very long
	all := duplicateControllers()

	for _, c := range all {
		err = c.Maintain(ctx)
		if err != nil {
			return
		}
	}
	return
}

// MaintainThorough runs the MaintainThorough method on all storages.
func MaintainThorough(ctx context.Context) (err error) {
	// copy, as we might use the very long
	all := duplicateControllers()

	for _, c := range all {
		err = c.MaintainThorough(ctx)
		if err != nil {
			return
		}
	}
	return
}

// MaintainRecordStates runs record state lifecycle maintenance on all storages.
func MaintainRecordStates(ctx context.Context) (err error) {
	// delete immediately for now
	// TODO: increase purge threshold when starting to sync DBs
	purgeDeletedBefore := time.Now().UTC()

	// copy, as we might use the very long
	all := duplicateControllers()

	for _, c := range all {
		err = c.MaintainRecordStates(ctx, purgeDeletedBefore)
		if err != nil {
			return
		}
	}
	return
}

func duplicateControllers() (all []*Controller) {
	controllersLock.RLock()
	defer controllersLock.RUnlock()

	all = make([]*Controller, 0, len(controllers))
	for _, c := range controllers {
		all = append(all, c)
	}

	return
}
