package process

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/safing/portmaster/base/api"
	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/service/network/netutils"
	"github.com/safing/portmaster/service/network/packet"
	"github.com/safing/portmaster/service/network/reference"
	"github.com/safing/portmaster/service/network/state"
	"github.com/safing/portmaster/service/profile"
)

// GetProcessWithProfile returns the process, including the profile.
// Always returns valid data.
// Errors are logged and returned for information or special handling purposes.
func GetProcessWithProfile(ctx context.Context, pid int) (process *Process, err error) {
	if !enableProcessDetection() {
		log.Tracer(ctx).Tracef("process: process detection disabled")
		return GetUnidentifiedProcess(ctx), nil
	}

	process, err = GetOrFindProcess(ctx, pid)
	if err != nil {
		log.Tracer(ctx).Debugf("process: failed to find process with PID: %s", err)
		return GetUnidentifiedProcess(ctx), err
	}

	// Get process group leader, which is the process "nearest" to the user and
	// will have more/better information for finding names ans icons, for example.
	err = process.FindProcessGroupLeader(ctx)
	if err != nil {
		log.Warningf("process: failed to get process group leader for %s: %s", process, err)
	}

	changed, err := process.GetProfile(ctx)
	if err != nil {
		log.Tracer(ctx).Errorf("process: failed to get profile for process %s: %s", process, err)
	}

	if changed {
		process.Save()
	}

	return process, nil
}

// GetPidOfConnection returns the PID of the process that owns the described connection.
// Always returns valid data.
// Errors are logged and returned for information or special handling purposes.
func GetPidOfConnection(ctx context.Context, pktInfo *packet.Info) (pid int, connInbound bool, err error) {
	if !enableProcessDetection() {
		return UnidentifiedProcessID, pktInfo.Inbound, nil
	}

	// Use fast search for inbound packets, as the listening socket should
	// already be there for a while now.
	fastSearch := pktInfo.Inbound
	connInbound = pktInfo.Inbound

	// Check if we need to get the PID.
	if pktInfo.PID == UndefinedProcessID {
		log.Tracer(ctx).Tracef("process: getting pid from system network state")
		pid, connInbound, err = state.Lookup(pktInfo, fastSearch)
		if err != nil {
			err = fmt.Errorf("failed to find PID of connection: %w", err)
			log.Tracer(ctx).Tracef("process: %s", err)
			pid = UndefinedProcessID
		}
	} else {
		log.Tracer(ctx).Tracef("process: pid already set in packet (by ebpf or kext)")
		pid = pktInfo.PID
	}

	// Fallback to special profiles if PID could not be found.
	if pid == UndefinedProcessID {
		switch {
		case !connInbound:
			pid = UnidentifiedProcessID

		case netutils.ClassifyIP(pktInfo.Dst).IsLocalhost():
			// Always treat localhost connections as unidentified/unknown.
			pid = UnidentifiedProcessID

		case reference.IsICMP(uint8(pktInfo.Protocol)):
			// Always treat ICMP as unidentified/unknown, as the direction
			// might change to outgoing by new ICMP echo packets.
			pid = UnidentifiedProcessID

		default:
			// All other inbound connections are "unsolicited".
			pid = UnsolicitedProcessID
		}
	}

	return pid, connInbound, err
}

// GetNetworkHost returns a *Process that represents a host on the network.
func GetNetworkHost(ctx context.Context, remoteIP net.IP) (process *Process, err error) { //nolint:interfacer
	now := time.Now().Unix()
	networkHost := &Process{
		Name:      fmt.Sprintf("Device at %s", remoteIP),
		UserName:  "N/A",
		UserID:    NetworkHostProcessID,
		Pid:       NetworkHostProcessID,
		ParentPid: NetworkHostProcessID,
		Tags: []profile.Tag{
			{
				Key:   "ip",
				Value: remoteIP.String(),
			},
		},
		FirstSeen: now,
		LastSeen:  now,
	}

	// Get the (linked) local profile.
	networkHostProfile, err := profile.GetLocalProfile("", networkHost.MatchingData(), networkHost.CreateProfileCallback)
	if err != nil {
		return nil, err
	}

	// Assign profile to process.
	networkHost.PrimaryProfileID = networkHostProfile.ScopedID()
	networkHost.profile = networkHostProfile.LayeredProfile()

	return networkHost, nil
}

// GetProcessByRequestOrigin returns the process that initiated the API request ar.
func GetProcessByRequestOrigin(ar *api.Request) (*Process, error) {
	// get remote IP/Port
	remoteIP, remotePort, err := netutils.ParseIPPort(ar.RemoteAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to get remote IP/Port: %w", err)
	}

	pkt := &packet.Info{
		Inbound:  false, // outbound as we are looking for the process of the source address
		Version:  packet.IPv4,
		Protocol: packet.TCP,
		Src:      remoteIP,   // source as in the process we are looking for
		SrcPort:  remotePort, // source as in the process we are looking for
		PID:      UndefinedProcessID,
	}

	pid, _, err := GetPidOfConnection(ar.Context(), pkt)
	if err != nil {
		return nil, err
	}

	proc, err := GetProcessWithProfile(ar.Context(), pid)
	if err != nil {
		return nil, err
	}

	return proc, nil
}
