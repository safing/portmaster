package ships

import (
	"context"
	"net"
	"strconv"
	"sync"

	"github.com/safing/portmaster/spn/hub"
)

// Builder is a factory that can build ships and piers of it's protocol.
type Builder struct {
	LaunchShip    func(ctx context.Context, transport *hub.Transport, ip net.IP) (Ship, error)
	EstablishPier func(transport *hub.Transport, dockingRequests chan Ship) (Pier, error)
}

var (
	registry     = make(map[string]*Builder)
	allProtocols []string
	registryLock sync.Mutex
)

// Register registers a new builder for a protocol.
func Register(protocol string, builder *Builder) {
	registryLock.Lock()
	defer registryLock.Unlock()

	registry[protocol] = builder
}

// GetBuilder returns the builder for the given protocol, or nil if it does not exist.
func GetBuilder(protocol string) *Builder {
	registryLock.Lock()
	defer registryLock.Unlock()

	builder, ok := registry[protocol]
	if !ok {
		return nil
	}
	return builder
}

// Protocols returns a slice with all registered protocol names. The return slice must not be edited.
func Protocols() []string {
	registryLock.Lock()
	defer registryLock.Unlock()

	return allProtocols
}

// portToA transforms the given port into a string.
func portToA(port uint16) string {
	return strconv.FormatUint(uint64(port), 10)
}
