package process

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/safing/portbase/log"
	"github.com/safing/portmaster/network/packet"
	"github.com/safing/portmaster/network/state"
	"github.com/safing/portmaster/profile"
)

// GetProcessByConnection returns the process that owns the described connection.
func GetProcessByConnection(ctx context.Context, pktInfo *packet.Info) (process *Process, connInbound bool, err error) {
	if !enableProcessDetection() {
		log.Tracer(ctx).Tracef("process: process detection disabled")
		return GetUnidentifiedProcess(ctx), pktInfo.Inbound, nil
	}

	// Use fast search for inbound packets, as the listening socket should
	// already be there for a while now.
	fastSearch := pktInfo.Inbound

	log.Tracer(ctx).Tracef("process: getting pid from system network state")
	var pid int
	pid, connInbound, err = state.Lookup(pktInfo, fastSearch)
	if err != nil {
		log.Tracer(ctx).Tracef("process: failed to find PID of connection: %s", err)
		return nil, pktInfo.Inbound, err
	}

	process, err = GetOrFindProcess(ctx, pid)
	if err != nil {
		log.Tracer(ctx).Debugf("process: failed to find (primary) process with PID: %s", err)
		return nil, connInbound, err
	}

	changed, err := process.GetProfile(ctx)
	if err != nil {
		log.Tracer(ctx).Errorf("process: failed to get profile for process %s: %s", process, err)
	}

	if changed {
		process.Save()
	}

	return process, connInbound, nil
}

// GetNetworkHost returns a *Process that represents a host on the network.
func GetNetworkHost(ctx context.Context, remoteIP net.IP) (process *Process, err error) { //nolint:interfacer
	now := time.Now().Unix()
	networkHost := &Process{
		Name:      fmt.Sprintf("Network Host %s", remoteIP),
		UserName:  "Unknown",
		UserID:    NetworkHostProcessID,
		Pid:       NetworkHostProcessID,
		ParentPid: NetworkHostProcessID,
		Path:      fmt.Sprintf("net:%s", remoteIP),
		FirstSeen: now,
		LastSeen:  now,
	}

	// Get the (linked) local profile.
	networkHostProfile, err := profile.GetProfile(profile.SourceNetwork, remoteIP.String(), "", false)
	if err != nil {
		return nil, err
	}

	// Assign profile to process.
	networkHost.LocalProfileKey = networkHostProfile.Key()
	networkHost.profile = networkHostProfile.LayeredProfile()

	if networkHostProfile.Name == "" {
		// Assign name and save.
		networkHostProfile.Name = networkHost.Name

		err := networkHostProfile.Save()
		if err != nil {
			log.Warningf("process: failed to save profile %s: %s", networkHostProfile.ScopedID(), err)
		}
	}

	return networkHost, nil
}
