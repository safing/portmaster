package process

import (
	"context"
	"syscall"

	"github.com/safing/portbase/log"
)

// SystemProcessID is the PID of the System/Kernel itself.
const SystemProcessID = 0

func GetProcessGroupLeader(ctx context.Context, pid int) (*Process, error) {
	pgid, err := GetProcessGroupID(ctx, pid)
	if err != nil {
		return nil, err
	}

	leader, err := GetOrFindProcess(ctx, pgid)
	if err == nil {
		log.Infof("[DBUG] found leader pid=%d pgid=%d", leader.Pid, leader.Pgid)
		return leader, nil
	}

	// this seems like a orphan process group so find the outermost parent
	// i.e. the first process in the group
	iter, err := GetOrFindProcess(ctx, pid)
	if err != nil {
		log.Infof("[DBUG] failed to get process for pid %d", pid)
		return nil, err
	}

	// This is already the leader
	if iter.Pid == pgid {
		log.Infof("[DBUG] iter pid=%d pgid=%d is already leader", pid, pgid)
		return iter, nil
	}

	for {
		next, err := GetOrFindProcess(ctx, iter.ParentPid)
		if err != nil {
			return nil, err
		}

		// If the parent process group ID of does not match
		// the pgid than iter is the first child of the process
		// group
		if next.Pgid != pgid {
			return iter, nil
		}

		iter = next
	}
}

func GetProcessGroupID(ctx context.Context, pid int) (int, error) {
	return syscall.Getpgid(pid)
}

/*
func init() {
	tracer, err := ebpf.New()
	if err != nil {
		panic(err)
	}

	go func() {
		file, _ := os.Create("/tmp/tracer.json")
		enc := json.NewEncoder(file)
		enc.SetIndent("", "  ")

		defer tracer.Close()
		for {
			evt, err := tracer.Read()
			if err != nil {
				log.Errorf("failed to read from execve tracer: %s", err)
				return
			}

			_ = enc.Encode(evt)
		}
	}()
}
*/
