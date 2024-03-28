package navigator

import (
	"sync"
	"time"

	"github.com/safing/portbase/database/record"
	"github.com/safing/portmaster/service/intel"
	"github.com/safing/portmaster/spn/hub"
)

// PinExport is the exportable version of a Pin.
type PinExport struct {
	record.Base
	sync.Mutex

	ID        string
	Name      string
	Map       string
	FirstSeen time.Time

	EntityV4 *intel.Entity
	EntityV6 *intel.Entity
	// TODO: add coords

	States        []string // From pin.State
	VerifiedOwner string
	HopDistance   int

	ConnectedTo   map[string]*LaneExport // Key is Hub ID.
	Route         []string               // Includes Home Hub and this Pin's ID.
	SessionActive bool

	Info   *hub.Announcement
	Status *hub.Status
}

// LaneExport is the exportable version of a Lane.
type LaneExport struct {
	HubID string

	// Capacity designates the available bandwidth between these Hubs.
	// It is specified in bit/s.
	Capacity int

	// Lateny designates the latency between these Hubs.
	// It is specified in nanoseconds.
	Latency time.Duration
}

// Export puts the Pin's information into an exportable format.
func (pin *Pin) Export() *PinExport {
	pin.Lock()
	defer pin.Unlock()

	// Shallow copy static values.
	export := &PinExport{
		ID:            pin.Hub.ID,
		Name:          pin.Hub.Info.Name,
		Map:           pin.Hub.Map,
		FirstSeen:     pin.Hub.FirstSeen,
		EntityV4:      pin.EntityV4,
		EntityV6:      pin.EntityV6,
		States:        pin.State.Export(),
		VerifiedOwner: pin.VerifiedOwner,
		HopDistance:   pin.HopDistance,
		SessionActive: pin.hasActiveTerminal() || pin.State.Has(StateIsHomeHub),
		Info:          pin.Hub.Info,   // Is updated as a whole, no need to copy.
		Status:        pin.Hub.Status, // Is updated as a whole, no need to copy.
	}

	// Export lanes.
	export.ConnectedTo = make(map[string]*LaneExport, len(pin.ConnectedTo))
	for key, lane := range pin.ConnectedTo {
		export.ConnectedTo[key] = &LaneExport{
			HubID:    lane.Pin.Hub.ID,
			Capacity: lane.Capacity,
			Latency:  lane.Latency,
		}
	}

	// Export route to Pin, if connected.
	if pin.Connection != nil && pin.Connection.Route != nil {
		export.Route = make([]string, len(pin.Connection.Route.Path))
		for key, hop := range pin.Connection.Route.Path {
			export.Route[key] = hop.HubID
		}
	}

	// Create database record metadata.
	export.SetKey(makeDBKey(export.Map, export.ID))
	export.SetMeta(&record.Meta{
		Created:  export.FirstSeen.Unix(),
		Modified: time.Now().Unix(),
	})

	return export
}
