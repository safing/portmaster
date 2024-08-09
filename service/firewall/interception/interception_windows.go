package interception

import (
	"fmt"
	"time"

	"github.com/safing/portmaster/base/log"
	kext1 "github.com/safing/portmaster/service/firewall/interception/windowskext"
	kext2 "github.com/safing/portmaster/service/firewall/interception/windowskext2"
	"github.com/safing/portmaster/service/mgr"
	"github.com/safing/portmaster/service/network"
	"github.com/safing/portmaster/service/network/packet"
	"github.com/safing/portmaster/service/updates"
)

var useOldKext = false

// start starts the interception.
func startInterception(packets chan packet.Packet) error {
	kextFile, err := updates.GetPlatformFile("kext/portmaster-kext.sys")
	if err != nil {
		return fmt.Errorf("interception: could not get kext sys: %s", err)
	}

	err = kext2.Init(kextFile.Path())
	if err != nil {
		return fmt.Errorf("interception: could not init windows kext: %s", err)
	}

	err = kext2.Start()
	if err != nil {
		return fmt.Errorf("interception: could not start windows kext: %s", err)
	}

	version, err := kext2.GetVersion()
	if err != nil {
		return fmt.Errorf("interception: failed to read version: %s", err)
	}
	log.Debugf("Kext version: %s", version.String())

	if version.Major < 2 {
		useOldKext = true

		// Transfer ownership.
		kext1.SetKextHandler(kext2.GetKextHandle())
		kext1.SetKextService(kext2.GetKextServiceHandle(), kextFile.Path())

		// Start packet handler.
		module.mgr.Go("kext packet handler", func(w *mgr.WorkerCtx) error {
			kext1.Handler(w.Ctx(), packets)
			return nil
		})

		// Start bandwidth stats monitor.
		module.mgr.Go("kext bandwidth stats monitor", func(w *mgr.WorkerCtx) error {
			return kext1.BandwidthStatsWorker(w.Ctx(), 1*time.Second, BandwidthUpdates)
		})
	} else {

		// Start packet handler.
		module.mgr.Go("kext packet handler", func(w *mgr.WorkerCtx) error {
			kext2.Handler(w.Ctx(), packets, BandwidthUpdates)
			return nil
		})

		// Start bandwidth stats monitor.
		module.mgr.Go("kext bandwidth request worker", func(w *mgr.WorkerCtx) error {
			timer := time.NewTicker(1 * time.Second)
			defer timer.Stop()
			for {
				select {
				case <-timer.C:
					err := kext2.SendBandwidthStatsRequest()
					if err != nil {
						return err
					}
				case <-w.Done():
					return nil
				}

			}
		})

		// Start kext logging. The worker will periodically send request to the kext to send logs.
		module.mgr.Go("kext log request worker", func(w *mgr.WorkerCtx) error {
			timer := time.NewTicker(1 * time.Second)
			defer timer.Stop()
			for {
				select {
				case <-timer.C:
					err := kext2.SendLogRequest()
					if err != nil {
						return err
					}
				case <-w.Done():
					return nil
				}

			}
		})

		module.mgr.Go("kext clean ended connection worker", func(w *mgr.WorkerCtx) error {
			timer := time.NewTicker(30 * time.Second)
			defer timer.Stop()
			for {
				select {
				case <-timer.C:
					err := kext2.SendCleanEndedConnection()
					if err != nil {
						return err
					}
				case <-w.Done():
					return nil
				}

			}
		})
	}

	return nil
}

// stop starts the interception.
func stopInterception() error {
	if useOldKext {
		return kext1.Stop()
	}
	return kext2.Stop()
}

// ResetVerdictOfAllConnections resets all connections so they are forced to go thought the firewall again.
func ResetVerdictOfAllConnections() error {
	if useOldKext {
		return kext1.ClearCache()
	}
	return kext2.ClearCache()
}

// UpdateVerdictOfConnection updates the verdict of the given connection in the kernel extension.
func UpdateVerdictOfConnection(conn *network.Connection) error {
	if useOldKext {
		return kext1.UpdateVerdict(conn)
	}
	return kext2.UpdateVerdict(conn)
}

// GetKextVersion returns the version of the kernel extension.
func GetKextVersion() (string, error) {
	if useOldKext {
		version, err := kext1.GetVersion()
		if err != nil {
			return "", err
		}
		return version.String(), nil
	} else {
		version, err := kext2.GetVersion()
		if err != nil {
			return "", err
		}
		return version.String(), nil
	}

}
