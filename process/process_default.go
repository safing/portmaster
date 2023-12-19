//go:build !windows && !linux
// +build !windows,!linux

package process

import (
	"context"
)

// SystemProcessID is the PID of the System/Kernel itself.
const SystemProcessID = 0

func GetProcessGroupLeader(ctx context.Context, pid int) (*Process, error) {
	// On systems other than linux we just return the process with PID == pid
	return GetOrFindProcess(ctx, pid)
}

func GetProcessGroupID(ctx context.Context, pid int) (int, error) {
	return 0
}
