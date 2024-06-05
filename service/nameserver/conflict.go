package nameserver

import (
	"net"
	"os"

	processInfo "github.com/shirou/gopsutil/process"

	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/service/network/packet"
	"github.com/safing/portmaster/service/network/state"
)

var commonResolverIPs = []net.IP{
	net.IPv4zero,
	net.IPv4(127, 0, 0, 1),  // default
	net.IPv4(127, 0, 0, 53), // some resolvers on Linux
	net.IPv6zero,
	net.IPv6loopback,
}

func findConflictingProcess(ip net.IP, port uint16) (conflictingProcess *processInfo.Process) {
	// Evaluate which IPs to check.
	var ipsToCheck []net.IP
	if ip.Equal(net.IPv4zero) || ip.Equal(net.IPv6zero) {
		ipsToCheck = commonResolverIPs
	} else {
		ipsToCheck = []net.IP{ip}
	}

	// Find the conflicting process.
	var err error
	for _, resolverIP := range ipsToCheck {
		conflictingProcess, err = getListeningProcess(resolverIP, port)
		switch {
		case err != nil:
			// Log the error and let the worker try again.
			log.Warningf("nameserver: failed to find conflicting service: %s", err)
		case conflictingProcess != nil:
			// Conflicting service found.
			return conflictingProcess
		}
	}

	return nil
}

func getListeningProcess(resolverIP net.IP, resolverPort uint16) (*processInfo.Process, error) {
	pid, _, err := state.Lookup(&packet.Info{
		Inbound:  true,
		Version:  0, // auto-detect
		Protocol: packet.UDP,
		Src:      nil, // do not record direction
		SrcPort:  0,   // do not record direction
		Dst:      resolverIP,
		DstPort:  resolverPort,
	}, true)
	if err != nil {
		// there may be nothing listening on :53
		return nil, nil //nolint:nilerr // Treat lookup error as "not found".
	}

	// Ignore if it's us for some reason.
	if pid == os.Getpid() {
		return nil, nil
	}

	proc, err := processInfo.NewProcess(int32(pid))
	if err != nil {
		// Process may have disappeared already.
		return nil, err
	}

	return proc, nil
}
