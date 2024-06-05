package navigator

import (
	"context"
	"net"
	"strings"
	"time"

	"github.com/tevino/abool"

	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/service/intel"
	"github.com/safing/portmaster/service/intel/geoip"
	"github.com/safing/portmaster/spn/docks"
	"github.com/safing/portmaster/spn/hub"
)

// Pin represents a Hub on a Map.
type Pin struct { //nolint:maligned
	// Hub Information
	Hub        *hub.Hub
	EntityV4   *intel.Entity
	EntityV6   *intel.Entity
	LocationV4 *geoip.Location
	LocationV6 *geoip.Location

	// Hub Status
	State PinState
	// VerifiedOwner holds the name of the verified owner / operator of the Hub.
	VerifiedOwner string
	// HopDistance signifies the needed hops to reach this Hub.
	// HopDistance is measured from the view of a client.
	// A Hub itself will have itself at distance 1.
	// Directly connected Hubs have a distance of 2.
	HopDistance int
	// Cost is the routing cost of this Hub.
	Cost float32
	// ConnectedTo holds validated lanes.
	ConnectedTo map[string]*Lane // Key is Hub ID.

	// FailingUntil specifies until when this Hub should be regarded as failing.
	// This is connected to StateFailing.
	FailingUntil time.Time

	// Connection holds a information about a connection to the Hub of this Pin.
	Connection *PinConnection

	// Internal

	// pushChanges is set to true if something noteworthy on the Pin changed and
	// an update needs to be pushed by the database storage interface to whoever
	// is listening.
	pushChanges *abool.AtomicBool

	// measurements holds Measurements regarding this Pin.
	// It must always be set and the reference must not be changed when measuring
	// is enabled.
	// Access to fields within are coordinated by itself.
	measurements *hub.Measurements

	// analysis holds the analysis state.
	// Should only be set during analysis and be reset at the start and removed at the end of an analysis.
	analysis *AnalysisState

	// region is the region this Pin belongs to.
	region *Region
}

// PinConnection represents a connection to a terminal on the Hub.
type PinConnection struct {
	// Terminal holds the active terminal session.
	Terminal *docks.ExpansionTerminal

	// Route is the route built for this terminal.
	Route *Route
}

// Lane is a connection to another Hub.
type Lane struct {
	// Pin is the Pin/Hub this Lane connects to.
	Pin *Pin

	// Capacity designates the available bandwidth between these Hubs.
	// It is specified in bit/s.
	Capacity int

	// Lateny designates the latency between these Hubs.
	// It is specified in nanoseconds.
	Latency time.Duration

	// Cost is the routing cost of this lane.
	Cost float32

	// active is a helper flag in order help remove abandoned Lanes.
	active bool
}

// Lock locks the Pin via the Hub's lock.
func (pin *Pin) Lock() {
	pin.Hub.Lock()
}

// Unlock unlocks the Pin via the Hub's lock.
func (pin *Pin) Unlock() {
	pin.Hub.Unlock()
}

// String returns a human-readable representation of the Pin.
func (pin *Pin) String() string {
	return "<Pin " + pin.Hub.Name() + ">"
}

// GetState returns the state of the pin.
func (pin *Pin) GetState() PinState {
	pin.Lock()
	defer pin.Unlock()

	return pin.State
}

// updateLocationData fetches the necessary location data in order to correctly map out the Pin.
func (pin *Pin) updateLocationData() {
	// TODO: We are currently assigning the Hub ID to the entity domain to
	// support matching a Hub by its ID. The issue here is that the domain
	// rules are lower-cased, so we have to lower-case the ID here too.
	// This is not optimal from a security perspective, but there are still
	// enough bits left that this cannot be easily exploited.

	if pin.Hub.Info.IPv4 != nil {
		pin.EntityV4 = (&intel.Entity{
			IP:     pin.Hub.Info.IPv4,
			Domain: strings.ToLower(pin.Hub.ID) + ".",
		}).Init(0)

		var ok bool
		pin.LocationV4, ok = pin.EntityV4.GetLocation(context.TODO())
		if !ok {
			log.Warningf("spn/navigator: failed to get location of %s of %s", pin.Hub.Info.IPv4, pin.Hub.StringWithoutLocking())
			return
		}
	} else {
		pin.EntityV4 = nil
		pin.LocationV4 = nil
	}

	if pin.Hub.Info.IPv6 != nil {
		pin.EntityV6 = (&intel.Entity{
			IP:     pin.Hub.Info.IPv6,
			Domain: strings.ToLower(pin.Hub.ID) + ".",
		}).Init(0)

		var ok bool
		pin.LocationV6, ok = pin.EntityV6.GetLocation(context.TODO())
		if !ok {
			log.Warningf("spn/navigator: failed to get location of %s of %s", pin.Hub.Info.IPv6, pin.Hub.StringWithoutLocking())
			return
		}
	} else {
		pin.EntityV6 = nil
		pin.LocationV6 = nil
	}
}

// GetLocation returns the geoip location of the Pin, preferring first the given IP, then IPv4.
func (pin *Pin) GetLocation(ip net.IP) *geoip.Location {
	pin.Lock()
	defer pin.Unlock()

	switch {
	case ip != nil && ip.Equal(pin.Hub.Info.IPv4) && pin.LocationV4 != nil:
		return pin.LocationV4
	case ip != nil && ip.Equal(pin.Hub.Info.IPv6) && pin.LocationV6 != nil:
		return pin.LocationV6
	case pin.LocationV4 != nil:
		return pin.LocationV4
	case pin.LocationV6 != nil:
		return pin.LocationV6
	default:
		return nil
	}
}

// SetActiveTerminal sets an active terminal for the pin.
func (pin *Pin) SetActiveTerminal(pc *PinConnection) {
	pin.Lock()
	defer pin.Unlock()

	pin.Connection = pc
	if pin.Connection != nil && pin.Connection.Terminal != nil {
		pin.Connection.Terminal.SetChangeNotifyFunc(pin.NotifyTerminalChange)
	}

	pin.pushChanges.Set()
}

// GetActiveTerminal returns the active terminal of the pin.
func (pin *Pin) GetActiveTerminal() *docks.ExpansionTerminal {
	pin.Lock()
	defer pin.Unlock()

	if !pin.hasActiveTerminal() {
		return nil
	}
	return pin.Connection.Terminal
}

// HasActiveTerminal returns whether the Pin has an active terminal.
func (pin *Pin) HasActiveTerminal() bool {
	pin.Lock()
	defer pin.Unlock()

	return pin.hasActiveTerminal()
}

func (pin *Pin) hasActiveTerminal() bool {
	return pin.Connection != nil &&
		pin.Connection.Terminal.Abandoning.IsNotSet()
}

// NotifyTerminalChange notifies subscribers of the changed terminal.
func (pin *Pin) NotifyTerminalChange() {
	pin.pushChanges.Set()
	pin.pushChange()
}

// IsFailing returns whether the pin should be treated as failing.
// The Pin is locked for this.
func (pin *Pin) IsFailing() bool {
	pin.Lock()
	defer pin.Unlock()

	return time.Now().Before(pin.FailingUntil)
}

// MarkAsFailingFor marks the pin as failing.
// The Pin is locked for this.
// Changes are pushed.
func (pin *Pin) MarkAsFailingFor(duration time.Duration) {
	pin.Lock()
	defer pin.Unlock()

	until := time.Now().Add(duration)
	// Only ever increase failing until, never reduce.
	if until.After(pin.FailingUntil) {
		pin.FailingUntil = until
	}

	pin.addStates(StateFailing)

	pin.pushChanges.Set()
	pin.pushChange()
}

// ResetFailingState resets the failing state.
// The Pin is locked for this.
// Changes are not pushed, but Pins are marked.
func (pin *Pin) ResetFailingState() {
	pin.Lock()
	defer pin.Unlock()

	if time.Now().Before(pin.FailingUntil) {
		pin.FailingUntil = time.Now()
		pin.pushChanges.Set()
	}
	if pin.State.Has(StateFailing) {
		pin.removeStates(StateFailing)
		pin.pushChanges.Set()
	}
}
