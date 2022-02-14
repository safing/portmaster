package nameserver

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"time"

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

	// lastKilledPID holds the PID of the last killed conflicting service.
	// It is only accessed by checkForConflictingService, which is only called by
	// the nameserver worker.
	lastKilledPID int
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
	var killingFailed bool
ipsToCheckLoop:
	for _, resolverIP := range ipsToCheck {
		pid, err := takeover(resolverIP, port)
		switch {
		case err != nil:
			// Log the error and let the worker try again.
			log.Infof("nameserver: failed to stop conflicting service: %s", err)
			killingFailed = true
			break ipsToCheckLoop
		case pid != 0:
			// Conflicting service identified and killed!
			killed = pid
			break ipsToCheckLoop
		}
	}

	// Notify user of failed killing or repeated kill.
	if killingFailed || (killed != 0 && killed == lastKilledPID) {
		// Notify the user that we failed to kill something.
		notifications.Notify(&notifications.Notification{
			EventID:      "namserver:failed-to-kill-conflicting-service",
			Type:         notifications.Error,
			Title:        "Failed to Stop Conflicting DNS Client",
			Message:      "The Portmaster failed to stop a conflicting DNS client to gain required system integration. If there is another DNS Client (Nameserver; Resolver) on this device, please disable it.",
			ShowOnSystem: true,
			AvailableActions: []*notifications.Action{
				{
					ID:   "ack",
					Text: "OK",
				},
				{
					Text:    "Open Docs",
					Type:    notifications.ActionTypeOpenURL,
					Payload: "https://docs.safing.io/portmaster/install/status/software-compatibility",
				},
			},
		})
		return nil
	}

	// Check if something was killed.
	if killed == 0 {
		return nil
	}
	lastKilledPID = killed

	// Notify the user that we killed something.
	notifications.Notify(&notifications.Notification{
		EventID: "namserver:stopped-conflicting-service",
		Type:    notifications.Info,
		Title:   "Stopped Conflicting DNS Client",
		Message: fmt.Sprintf(
			"The Portmaster stopped a conflicting DNS client (pid %d) to gain required system integration. If you are running another DNS client on this device on purpose, you can the check the documentation if it is compatible with the Portmaster.",
			killed,
		),
		ShowOnSystem: true,
		AvailableActions: []*notifications.Action{
			{
				ID:   "ack",
				Text: "OK",
			},
			{
				Text:    "Open Docs",
				Type:    notifications.ActionTypeOpenURL,
				Payload: "https://docs.safing.io/portmaster/install/status/software-compatibility",
			},
		},
	})

	// Restart nameserver via service-worker logic.
	// Wait shortly so that the other process can shut down.
	time.Sleep(10 * time.Millisecond)
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
	}, true)
	if err != nil {
		// there may be nothing listening on :53
		return 0, nil //nolint:nilerr // Treat lookup error as "not found".
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
