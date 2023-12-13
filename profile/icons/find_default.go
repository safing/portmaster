//go:build !linux

package icons

import "context"

// FindIcon returns nil, nil for unsupported platforms.
func FindIcon(ctx context.Context, binName string, homeDir string) (*Icon, error) {
	return nil, nil
}
