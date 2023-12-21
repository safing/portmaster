//go:build !windows && !linux
// +build !windows,!linux

package process

import (
	"context"
)

// SystemProcessID is the PID of the System/Kernel itself.
const SystemProcessID = 0

// GetProcessGroupLeader returns the process that leads the process group.
// Returns nil on unsupported platforms.
func (p *Process) FindProcessGroupLeader(ctx context.Context) error {
	return nil
}

// GetProcessGroupID returns the process group ID of the given PID.
// Returns undefined process ID on unsupported platforms.
func GetProcessGroupID(ctx context.Context, pid int) (int, error) {
	return UndefinedProcessID, nil
}
