package netenv

import (
	"bytes"
	"context"
	"crypto/sha1" //nolint:gosec // not used for security
	"io"
	"net"
	"time"

	"github.com/safing/portbase/log"
	"github.com/safing/portbase/utils"
)

var (
	networkChangeCheckTrigger    = make(chan struct{}, 1)
	networkChangedFlagController = utils.NewFlagController()
)

func GetNetworkChangedFlag() *utils.Flag {
	return networkChangedFlagController.NewFlag()
}

func notifyOfNetworkChange() {
	networkChangedFlagController.NotifyAndReset()
	module.TriggerEvent(NetworkChangedEvent, nil)
}

func triggerNetworkChangeCheck() {
	select {
	case networkChangeCheckTrigger <- struct{}{}:
	default:
	}
}

func monitorNetworkChanges(ctx context.Context) error {
	var lastNetworkChecksum []byte

serviceLoop:
	for {
		trigger := false

		timeout := time.Minute
		if GetOnlineStatus() != StatusOnline {
			timeout = time.Second
		}
		// wait for trigger
		select {
		case <-ctx.Done():
			return nil
		case <-networkChangeCheckTrigger:
			// don't fall through because the online change check
			// triggers the networkChangeCheck this way. If we would set
			// trigger == true we would trigger the online check again
			// resulting in a loop of pointless checks.
		case <-time.After(timeout):
			trigger = true
		}

		// check network for changes
		// create hashsum of current network config
		hasher := sha1.New() //nolint:gosec // not used for security
		interfaces, err := net.Interfaces()
		if err != nil {
			log.Warningf("environment: failed to get interfaces: %s", err)
			continue
		}
		for _, iface := range interfaces {
			_, _ = io.WriteString(hasher, iface.Name)
			// log.Tracef("adding: %s", iface.Name)
			_, _ = io.WriteString(hasher, iface.Flags.String())
			// log.Tracef("adding: %s", iface.Flags.String())
			addrs, err := iface.Addrs()
			if err != nil {
				log.Warningf("environment: failed to get addrs from interface %s: %s", iface.Name, err)
				continue
			}
			for _, addr := range addrs {
				_, _ = io.WriteString(hasher, addr.String())
				// log.Tracef("adding: %s", addr.String())
			}
		}
		newChecksum := hasher.Sum(nil)

		// compare checksum with last
		if !bytes.Equal(lastNetworkChecksum, newChecksum) {
			if len(lastNetworkChecksum) == 0 {
				lastNetworkChecksum = newChecksum
				continue serviceLoop
			}
			lastNetworkChecksum = newChecksum

			if trigger {
				triggerOnlineStatusInvestigation()
			}
			notifyOfNetworkChange()
		}

	}
}
