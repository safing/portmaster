package conf

import (
	"sync/atomic"
)

var (
	publicHub  atomic.Bool
	client     atomic.Bool
	integrated atomic.Bool
)

// PublicHub returns whether this is a public Hub.
func PublicHub() bool {
	return publicHub.Load()
}

// EnablePublicHub enables the public hub mode.
func EnablePublicHub(enable bool) {
	publicHub.Store(enable)
}

// Client returns whether this is a client.
func Client() bool {
	return client.Load()
}

// EnableClient enables the client mode.
func EnableClient(enable bool) {
	client.Store(enable)
}

// Integrated returns whether SPN is running integrated into Portmaster.
func Integrated() bool {
	return integrated.Load()
}

// EnableIntegration enables the integrated mode.
func EnableIntegration(enable bool) {
	integrated.Store(enable)
}
