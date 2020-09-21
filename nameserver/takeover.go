package nameserver

import (
	"fmt"
	"net"
	"os"
	"time"

	"github.com/safing/portbase/log"
	"github.com/safing/portbase/modules"
	"github.com/safing/portbase/notifications"
	"github.com/safing/portmaster/network/packet"
	"github.com/safing/portmaster/network/state"
)

var (
	otherResolverIPs = []net.IP{
		net.IPv4(127, 0, 0, 1),  // default
		net.IPv4(127, 0, 0, 53), // some resolvers on Linux
	}
)

func checkForConflictingService() error {
	var pid int
	var err error

	// check multiple IPs for other resolvers
	for _, resolverIP := range otherResolverIPs {
		pid, err = takeover(resolverIP)
		if err == nil && pid != 0 {
			break
		}
	}
	// handle returns
	if err != nil {
		log.Infof("nameserver: could not stop conflicting service: %s", err)
		// leave original service-worker error intact
		return nil
	}
	if pid == 0 {
		// no conflicting service identified
		return nil
	}

	// we killed something!

	// wait for a short duration for the other service to shut down
	time.Sleep(10 * time.Millisecond)

	notifications.NotifyInfo(
		"namserver-stopped-conflicting-service",
		fmt.Sprintf("Portmaster stopped a conflicting name service (pid %d) to gain required system integration.", pid),
	)

	// restart via service-worker logic
	return fmt.Errorf("%w: stopped conflicting name service with pid %d", modules.ErrRestartNow, pid)
}

func takeover(resolverIP net.IP) (int, error) {
	pid, _, err := state.Lookup(&packet.Info{
		Inbound:  true,
		Version:  0, // auto-detect
		Protocol: packet.UDP,
		Src:      nil, // do not record direction
		SrcPort:  0,   // do not record direction
		Dst:      resolverIP,
		DstPort:  53,
	})
	if err != nil {
		// there may be nothing listening on :53
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

	return pid, nil
}
