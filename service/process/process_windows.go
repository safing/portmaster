package process

import (
	"context"
)

// SystemProcessID is the PID of the System/Kernel itself.
const SystemProcessID = 4

// GetProcessGroupLeader returns the process that leads the process group.
// Returns nil on Windows, as it does not have process groups.
func (p *Process) FindProcessGroupLeader(ctx context.Context) error {
	// TODO: Get "main" process of process job object.
	return nil
}

// GetProcessGroupID returns the process group ID of the given PID.
// Returns the undefined process ID on Windows, as it does not have process groups.
func GetProcessGroupID(ctx context.Context, pid int) (int, error) {
	return UndefinedProcessID, nil
}
