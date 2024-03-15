//go:build !linux && !windows

package binmeta

import "context"

// GetIconAndName returns zero values for unsupported platforms.
func GetIconAndName(ctx context.Context, binPath string, homeDir string) (icon *Icon, name string, err error) {
	return nil, "", nil
}
