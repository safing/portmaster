package ships

import (
	"fmt"
	"net"

	"github.com/tevino/abool"

	"github.com/safing/portmaster/spn/hub"
)

// Pier represents a network connection listener.
type Pier interface {
	// String returns a human readable informational summary about the ship.
	String() string

	// Transport returns the transport used for this ship.
	Transport() *hub.Transport

	// Abolish closes the underlying listener and cleans up any related resources.
	Abolish()
}

// DockingRequest is a uniform request that Piers emit when a new ship arrives.
type DockingRequest struct {
	Pier Pier
	Ship Ship
	Err  error
}

// EstablishPier is shorthand function to get the transport's builder and establish a pier.
func EstablishPier(transport *hub.Transport, dockingRequests chan Ship) (Pier, error) {
	builder := GetBuilder(transport.Protocol)
	if builder == nil {
		return nil, fmt.Errorf("protocol %s not supported", transport.Protocol)
	}

	pier, err := builder.EstablishPier(transport, dockingRequests)
	if err != nil {
		return nil, fmt.Errorf("failed to establish pier on %s: %w", transport, err)
	}

	return pier, nil
}

// PierBase implements common functions to comply with the Pier interface.
type PierBase struct {
	// transport holds the transport definition of the pier.
	transport *hub.Transport
	// listeners holds the actual underlying listeners.
	listeners []net.Listener

	// dockingRequests is used to report new connections to the higher layer.
	dockingRequests chan Ship

	// abolishing specifies if the pier and listener is being closed.
	abolishing *abool.AtomicBool
}

func (pier *PierBase) initBase() {
	// init
	pier.abolishing = abool.New()
}

// String returns a human readable informational summary about the ship.
func (pier *PierBase) String() string {
	return fmt.Sprintf("<Pier %s>", pier.transport)
}

// Transport returns the transport used for this ship.
func (pier *PierBase) Transport() *hub.Transport {
	return pier.transport
}

// Abolish closes the underlying listener and cleans up any related resources.
func (pier *PierBase) Abolish() {
	if pier.abolishing.SetToIf(false, true) {
		for _, listener := range pier.listeners {
			_ = listener.Close()
		}
	}
}
