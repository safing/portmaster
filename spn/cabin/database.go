package cabin

import (
	"errors"
	"fmt"

	"github.com/safing/portmaster/base/database"
	"github.com/safing/portmaster/base/database/record"
	"github.com/safing/portmaster/spn/hub"
)

var db = database.NewInterface(nil)

// LoadIdentity loads an identify with the given key.
func LoadIdentity(key string) (id *Identity, changed bool, err error) {
	r, err := db.Get(key)
	if err != nil {
		return nil, false, err
	}
	id, err = EnsureIdentity(r)
	if err != nil {
		return nil, false, fmt.Errorf("failed to parse identity: %w", err)
	}

	// Check if required fields are present.
	switch {
	case id.Hub == nil:
		return nil, false, errors.New("missing id.Hub")
	case id.Signet == nil:
		return nil, false, errors.New("missing id.Signet")
	case id.Hub.Info == nil:
		return nil, false, errors.New("missing hub.Info")
	case id.Hub.Status == nil:
		return nil, false, errors.New("missing hub.Status")
	case id.ID != id.Hub.ID:
		return nil, false, errors.New("hub.ID mismatch")
	case id.ID != id.Hub.Info.ID:
		return nil, false, errors.New("hub.Info.ID mismatch")
	case id.Map == "":
		return nil, false, errors.New("invalid id.Map")
	case id.Hub.Map == "":
		return nil, false, errors.New("invalid hub.Map")
	case id.Hub.FirstSeen.IsZero():
		return nil, false, errors.New("missing hub.FirstSeen")
	case id.Hub.Info.Timestamp == 0:
		return nil, false, errors.New("missing hub.Info.Timestamp")
	case id.Hub.Status.Timestamp == 0:
		return nil, false, errors.New("missing hub.Status.Timestamp")
	}

	// Run a initial maintenance routine.
	infoChanged, err := id.MaintainAnnouncement(nil, true)
	if err != nil {
		return nil, false, fmt.Errorf("failed to initialize announcement: %w", err)
	}
	statusChanged, err := id.MaintainStatus(nil, nil, nil, true)
	if err != nil {
		return nil, false, fmt.Errorf("failed to initialize status: %w", err)
	}

	// Ensure the Measurements reset the values.
	measurements := id.Hub.GetMeasurements()
	measurements.SetLatency(0)
	measurements.SetCapacity(0)
	measurements.SetCalculatedCost(hub.MaxCalculatedCost)

	return id, infoChanged || statusChanged, nil
}

// EnsureIdentity makes sure a database record is an Identity.
func EnsureIdentity(r record.Record) (*Identity, error) {
	// unwrap
	if r.IsWrapped() {
		// only allocate a new struct, if we need it
		id := &Identity{}
		err := record.Unwrap(r, id)
		if err != nil {
			return nil, err
		}
		return id, nil
	}

	// or adjust type
	id, ok := r.(*Identity)
	if !ok {
		return nil, fmt.Errorf("record not of type *Identity, but %T", r)
	}
	return id, nil
}

// Save saves the Identity to the database.
func (id *Identity) Save() error {
	if !id.KeyIsSet() {
		return errors.New("no key set")
	}

	return db.Put(id)
}
