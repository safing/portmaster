//go:build !windows

package core

// only return on Fatal error!
func startPlatformSpecific() error {
	return nil
}
