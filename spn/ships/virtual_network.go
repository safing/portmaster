package ships

import (
	"net"
	"sync"

	"github.com/safing/portmaster/spn/hub"
)

var (
	virtNetLock   sync.Mutex
	virtNetConfig *hub.VirtualNetworkConfig
)

// SetVirtualNetworkConfig sets the virtual networking config.
func SetVirtualNetworkConfig(config *hub.VirtualNetworkConfig) {
	virtNetLock.Lock()
	defer virtNetLock.Unlock()

	virtNetConfig = config
}

// GetVirtualNetworkConfig returns the virtual networking config.
func GetVirtualNetworkConfig() *hub.VirtualNetworkConfig {
	virtNetLock.Lock()
	defer virtNetLock.Unlock()

	return virtNetConfig
}

// GetVirtualNetworkAddress returns the virtual network IP for the given Hub.
func GetVirtualNetworkAddress(dstHubID string) net.IP {
	virtNetLock.Lock()
	defer virtNetLock.Unlock()

	// Check if we have a virtual network config.
	if virtNetConfig == nil {
		return nil
	}

	// Return mapping for given Hub ID.
	return virtNetConfig.Mapping[dstHubID]
}
