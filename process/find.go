package process

import (
	"context"
	"errors"
	"net"

	"github.com/safing/portbase/log"
	"github.com/safing/portmaster/network/packet"
)

// Errors
var (
	ErrConnectionNotFound = errors.New("could not find connection in system state tables")
	ErrProcessNotFound    = errors.New("could not find process in system state tables")
)

// GetPidByPacket returns the pid of the owner of the packet.
func GetPidByPacket(pkt packet.Packet) (pid int, direction bool, err error) {

	var localIP net.IP
	var localPort uint16
	var remoteIP net.IP
	var remotePort uint16
	if pkt.IsInbound() {
		localIP = pkt.Info().Dst
		remoteIP = pkt.Info().Src
	} else {
		localIP = pkt.Info().Src
		remoteIP = pkt.Info().Dst
	}
	if pkt.HasPorts() {
		if pkt.IsInbound() {
			localPort = pkt.Info().DstPort
			remotePort = pkt.Info().SrcPort
		} else {
			localPort = pkt.Info().SrcPort
			remotePort = pkt.Info().DstPort
		}
	}

	switch {
	case pkt.Info().Protocol == packet.TCP && pkt.Info().Version == packet.IPv4:
		return getTCP4PacketInfo(localIP, localPort, remoteIP, remotePort, pkt.IsInbound())
	case pkt.Info().Protocol == packet.UDP && pkt.Info().Version == packet.IPv4:
		return getUDP4PacketInfo(localIP, localPort, remoteIP, remotePort, pkt.IsInbound())
	case pkt.Info().Protocol == packet.TCP && pkt.Info().Version == packet.IPv6:
		return getTCP6PacketInfo(localIP, localPort, remoteIP, remotePort, pkt.IsInbound())
	case pkt.Info().Protocol == packet.UDP && pkt.Info().Version == packet.IPv6:
		return getUDP6PacketInfo(localIP, localPort, remoteIP, remotePort, pkt.IsInbound())
	default:
		return -1, false, errors.New("unsupported protocol for finding process")
	}

}

// GetProcessByPacket returns the process that owns the given packet.
func GetProcessByPacket(pkt packet.Packet) (process *Process, direction bool, err error) {
	log.Tracer(pkt.Ctx()).Tracef("process: getting process and profile by packet")

	var pid int
	pid, direction, err = GetPidByPacket(pkt)
	if err != nil {
		log.Tracer(pkt.Ctx()).Errorf("process: failed to find PID of connection: %s", err)
		return nil, direction, err
	}
	if pid < 0 {
		log.Tracer(pkt.Ctx()).Errorf("process: %s", ErrConnectionNotFound.Error())
		return nil, direction, ErrConnectionNotFound
	}

	process, err = GetOrFindPrimaryProcess(pkt.Ctx(), pid)
	if err != nil {
		log.Tracer(pkt.Ctx()).Errorf("process: failed to find (primary) process with PID: %s", err)
		return nil, direction, err
	}

	err = process.FindProfiles(pkt.Ctx())
	if err != nil {
		log.Tracer(pkt.Ctx()).Errorf("process: failed to find profiles for process %s: %s", process, err)
		log.Errorf("failed to find profiles for process %s: %s", process, err)
	}

	return process, direction, nil

}

// GetPidByEndpoints returns the pid of the owner of the described link.
func GetPidByEndpoints(localIP net.IP, localPort uint16, remoteIP net.IP, remotePort uint16, protocol packet.IPProtocol) (pid int, direction bool, err error) {

	ipVersion := packet.IPv4
	if v4 := localIP.To4(); v4 == nil {
		ipVersion = packet.IPv6
	}

	switch {
	case protocol == packet.TCP && ipVersion == packet.IPv4:
		return getTCP4PacketInfo(localIP, localPort, remoteIP, remotePort, false)
	case protocol == packet.UDP && ipVersion == packet.IPv4:
		return getUDP4PacketInfo(localIP, localPort, remoteIP, remotePort, false)
	case protocol == packet.TCP && ipVersion == packet.IPv6:
		return getTCP6PacketInfo(localIP, localPort, remoteIP, remotePort, false)
	case protocol == packet.UDP && ipVersion == packet.IPv6:
		return getUDP6PacketInfo(localIP, localPort, remoteIP, remotePort, false)
	default:
		return -1, false, errors.New("unsupported protocol for finding process")
	}

}

// GetProcessByEndpoints returns the process that owns the described link.
func GetProcessByEndpoints(ctx context.Context, localIP net.IP, localPort uint16, remoteIP net.IP, remotePort uint16, protocol packet.IPProtocol) (process *Process, err error) {
	log.Tracer(ctx).Tracef("process: getting process and profile by endpoints")

	var pid int
	pid, _, err = GetPidByEndpoints(localIP, localPort, remoteIP, remotePort, protocol)
	if err != nil {
		log.Tracer(ctx).Errorf("process: failed to find PID of connection: %s", err)
		return nil, err
	}
	if pid < 0 {
		log.Tracer(ctx).Errorf("process: %s", ErrConnectionNotFound.Error())
		return nil, ErrConnectionNotFound
	}

	process, err = GetOrFindPrimaryProcess(ctx, pid)
	if err != nil {
		log.Tracer(ctx).Errorf("process: failed to find (primary) process with PID: %s", err)
		return nil, err
	}

	err = process.FindProfiles(ctx)
	if err != nil {
		log.Tracer(ctx).Errorf("process: failed to find profiles for process %s: %s", process, err)
		log.Errorf("process: failed to find profiles for process %s: %s", process, err)
	}

	return process, nil
}

// GetActiveConnectionIDs returns a list of all active connection IDs.
func GetActiveConnectionIDs() []string {
	return getActiveConnectionIDs()
}
