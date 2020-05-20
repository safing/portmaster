package process

import (
	"context"
	"errors"

	"github.com/safing/portmaster/network/state"

	"github.com/safing/portbase/log"
	"github.com/safing/portmaster/network/packet"
)

// Errors
var (
	ErrProcessNotFound = errors.New("could not find process in system state tables")
)

// GetProcessByConnection returns the process that owns the described connection.
func GetProcessByConnection(ctx context.Context, pktInfo *packet.Info) (process *Process, connInbound bool, err error) {
	if !enableProcessDetection() {
		log.Tracer(ctx).Tracef("process: process detection disabled")
		return GetUnidentifiedProcess(ctx), pktInfo.Inbound, nil
	}

	log.Tracer(ctx).Tracef("process: getting pid from system network state")
	var pid int
	pid, connInbound, err = state.Lookup(pktInfo)
	if err != nil {
		log.Tracer(ctx).Debugf("process: failed to find PID of connection: %s", err)
		return nil, connInbound, err
	}

	process, err = GetOrFindPrimaryProcess(ctx, pid)
	if err != nil {
		log.Tracer(ctx).Debugf("process: failed to find (primary) process with PID: %s", err)
		return nil, connInbound, err
	}

	err = process.GetProfile(ctx)
	if err != nil {
		log.Tracer(ctx).Errorf("process: failed to get profile for process %s: %s", process, err)
	}

	return process, connInbound, nil
}
