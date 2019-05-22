package nameserver

import (
	"fmt"
	"net"
	"os"
	"time"

	"github.com/Safing/portbase/log"
	"github.com/Safing/portbase/notifications"
	"github.com/Safing/portmaster/network/packet"
	"github.com/Safing/portmaster/process"
)

func checkForConflictingService(err error) {
	pid, err := takeover()
	if err != nil || pid == 0 {
		log.Info("nameserver: restarting server in 10 seconds")
		time.Sleep(10 * time.Second)
		return
	}

	log.Infof("nameserver: stopped conflicting name service with pid %d", pid)

	// notify user
	(&notifications.Notification{
		ID:      "nameserver-stopped-conflicting-service",
		Message: fmt.Sprintf("Portmaster stopped a conflicting name service (pid %d) to gain required system integration.", pid),
	}).Init().Save()

	// wait for a short duration for the other service to shut down
	time.Sleep(100 * time.Millisecond)
}

func takeover() (int, error) {
	pid, _, err := process.GetPidByEndpoints(net.IPv4(127, 0, 0, 1), 53, net.IPv4(127, 0, 0, 1), 65535, packet.UDP)
	if err != nil {
		// there may be nothing listening on :53
		log.Tracef("nameserver: expected conflicting name service, but could not find anything listenting on :53")
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
