package process

import (
	"context"
	"errors"
	"net"

	"github.com/safing/portmaster/network/state"

	"github.com/safing/portbase/log"
	"github.com/safing/portmaster/network/packet"
)

// Errors
var (
	ErrProcessNotFound = errors.New("could not find process in system state tables")
)

// GetProcessByPacket returns the process that owns the given packet.
func GetProcessByPacket(pkt packet.Packet) (process *Process, inbound bool, err error) {
	meta := pkt.Info()
	return GetProcessByEndpoints(
		pkt.Ctx(),
		meta.Version,
		meta.Protocol,
		meta.LocalIP(),
		meta.LocalPort(),
		meta.RemoteIP(),
		meta.RemotePort(),
		meta.Direction,
	)
}

// GetProcessByEndpoints returns the process that owns the described link.
func GetProcessByEndpoints(
	ctx context.Context,
	ipVersion packet.IPVersion,
	protocol packet.IPProtocol,
	localIP net.IP,
	localPort uint16,
	remoteIP net.IP,
	remotePort uint16,
	pktInbound bool,
) (
	process *Process,
	connInbound bool,
	err error,
) {

	if !enableProcessDetection() {
		log.Tracer(ctx).Tracef("process: process detection disabled")
		return GetUnidentifiedProcess(ctx), pktInbound, nil
	}

	log.Tracer(ctx).Tracef("process: getting pid from system network state")
	var pid int
	pid, connInbound, err = state.Lookup(ipVersion, protocol, localIP, localPort, remoteIP, remotePort, pktInbound)
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
