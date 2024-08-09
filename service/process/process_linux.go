package process

import (
	"context"
	"fmt"
	"syscall"

	"github.com/safing/portmaster/base/log"
)

const (
	// SystemProcessID is the PID of the System/Kernel itself.
	SystemProcessID = 0

	// SystemInitID is the PID of the system init process.
	SystemInitID = 1
)

// FindProcessGroupLeader returns the process that leads the process group.
// Returns nil when process ID is not valid (or virtual).
// If the process group leader is found, it is set on the process.
// If that process does not exist anymore, then the highest existing parent process is returned.
// If an error occurs, the best match is set.
func (p *Process) FindProcessGroupLeader(ctx context.Context) error {
	p.Lock()
	defer p.Unlock()

	// Return the leader if we already have it.
	if p.leader != nil {
		return nil
	}

	// Check if we have the process group leader PID.
	if p.LeaderPid == UndefinedProcessID {
		return nil
	}

	// Return nil if we already are the leader.
	if p.LeaderPid == p.Pid {
		return nil
	}

	// Get process leader process.
	leader, err := GetOrFindProcess(ctx, p.LeaderPid)
	if err == nil {
		p.leader = leader
		log.Tracer(ctx).Debugf("process: found process leader of %d: pid=%d pgid=%d", p.Pid, leader.Pid, leader.LeaderPid)
		return nil
	}

	// If we can't get the process leader process, it has likely already exited.
	// In that case, find the highest existing parent process within the process group.
	var (
		nextParentPid = p.ParentPid
		lastParent    *Process
	)
	for {
		// Get next parent.
		parent, err := GetOrFindProcess(ctx, nextParentPid)
		if err != nil {
			p.leader = lastParent
			return fmt.Errorf("failed to find parent %d: %w", nextParentPid, err)
		}

		// Check if we are ready to return.
		switch {
		case parent.Pid == p.LeaderPid:
			// Found the process group leader!
			p.leader = parent
			return nil

		case parent.LeaderPid != p.LeaderPid:
			// We are leaving the process group. Return the previous parent.
			p.leader = lastParent
			log.Tracer(ctx).Debugf("process: found process leader (highest parent) of %d: pid=%d pgid=%d", p.Pid, parent.Pid, parent.LeaderPid)
			return nil

		case parent.ParentPid == SystemProcessID,
			parent.ParentPid == SystemInitID:
			// Next parent is system or init.
			// Use current parent.
			p.leader = parent
			log.Tracer(ctx).Debugf("process: found process leader (highest parent) of %d: pid=%d pgid=%d", p.Pid, parent.Pid, parent.LeaderPid)
			return nil
		}

		// Check next parent.
		lastParent = parent
		nextParentPid = parent.ParentPid
	}
}

// GetProcessGroupID returns the process group ID of the given PID.
func GetProcessGroupID(ctx context.Context, pid int) (int, error) {
	return syscall.Getpgid(pid)
}
