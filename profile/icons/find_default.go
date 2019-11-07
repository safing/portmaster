//go:build !linux

package icons

import "github.com/safing/portmaster/profile"

// FindIcon returns nil, nil for unsupported platforms.
func FindIcon(binName string, homeDir string) (*profile.Icon, error) {
	return nil, nil
}
