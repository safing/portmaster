package nameserver

import (
	"fmt"
	"net"
	"os"
	"strconv"

	"github.com/safing/portbase/log"
	"github.com/safing/portbase/modules"
	"github.com/safing/portbase/notifications"
	"github.com/safing/portmaster/network/packet"
	"github.com/safing/portmaster/network/state"
)

var (
	commonResolverIPs = []net.IP{
		net.IPv4zero,
		net.IPv4(127, 0, 0, 1),  // default
		net.IPv4(127, 0, 0, 53), // some resolvers on Linux
		net.IPv6zero,
		net.IPv6loopback,
	}
)

func checkForConflictingService(ip net.IP, port uint16) error {
	// Evaluate which IPs to check.
	var ipsToCheck []net.IP
	if ip.Equal(net.IPv4zero) || ip.Equal(net.IPv6zero) {
		ipsToCheck = commonResolverIPs
	} else {
		ipsToCheck = []net.IP{ip}
	}

	// Check if there is another resolver when need to take over.
	var killed int
	for _, resolverIP := range ipsToCheck {
		pid, err := takeover(resolverIP, port)
		switch {
		case err != nil:
			// Log the error and let the worker try again.
			log.Infof("nameserver: could not stop conflicting service: %s", err)
			return nil
		case pid != 0:
			// Conflicting service identified and killed!
			killed = pid
			break
		}
	}

	// Check if something was killed.
	if killed == 0 {
		return nil
	}

	// Notify the user that we killed something.
	notifications.Notify(&notifications.Notification{
		EventID:  "namserver:stopped-conflicting-service",
		Type:     notifications.Info,
		Title:    "Conflicting DNS Service",
		Category: "Secure DNS",
		Message: fmt.Sprintf(
			"The Portmaster stopped a conflicting name service (pid %d) to gain required system integration.",
			killed,
		),
	})

	// Restart nameserver via service-worker logic.
	return fmt.Errorf("%w: stopped conflicting name service with pid %d", modules.ErrRestartNow, killed)
}

func takeover(resolverIP net.IP, resolverPort uint16) (int, error) {
	pid, _, err := state.Lookup(&packet.Info{
		Inbound:  true,
		Version:  0, // auto-detect
		Protocol: packet.UDP,
		Src:      nil, // do not record direction
		SrcPort:  0,   // do not record direction
		Dst:      resolverIP,
		DstPort:  resolverPort,
	})
	if err != nil {
		// there may be nothing listening on :53
		return 0, nil
	}

	// Just don't, uh, kill ourselves...
	if pid == os.Getpid() {
		return 0, nil
	}

	proc, err := os.FindProcess(pid)
	if err != nil {
		// huh. gone already? I guess we'll wait then...
		return 0, err
	}

	err = proc.Signal(os.Interrupt)
	if err != nil {
		err = proc.Kill()
		if err != nil {
			log.Errorf("nameserver: failed to stop conflicting service (pid %d): %s", pid, err)
			return 0, err
		}
	}

	log.Warningf(
		"nameserver: killed conflicting service with PID %d over %s",
		pid,
		net.JoinHostPort(
			resolverIP.String(),
			strconv.Itoa(int(resolverPort)),
		),
	)

	return pid, nil
}
