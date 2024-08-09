//go:build windows
// +build windows

package windowskext

import (
	"fmt"

	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/service/network"
	"github.com/safing/portmaster/windows_kext/kextinterface"
	"golang.org/x/sys/windows"
)

// Package errors
var (
	driverPath string

	service  *kextinterface.KextService
	kextFile *kextinterface.KextFile
)

const (
	driverName = "PortmasterKext"
)

func Init(path string) error {
	driverPath = path
	return nil
}

// Start intercepting.
func Start() error {
	// initialize and start driver service
	var err error
	service, err = kextinterface.CreateKextService(driverName, driverPath)
	if err != nil {
		return fmt.Errorf("failed to create service: %w", err)
	}

	// Start service and open file
	err = service.Start(true)
	if err != nil {
		log.Errorf("failed to start service: %s", err)
	}

	kextFile, err = service.OpenFile(1024)
	if err != nil {
		return fmt.Errorf("failed to open driver: %w", err)
	}

	return nil
}

func GetKextHandle() windows.Handle {
	return kextFile.GetHandle()
}

func GetKextServiceHandle() windows.Handle {
	return service.GetHandle()
}

// Stop intercepting.
func Stop() error {
	// Prepare kernel for shutdown
	err := shutdownRequest()
	if err != nil {
		log.Warningf("winkext: shutdown request failed: %s", err)
	}
	// Close the interface to the driver. Driver will continue to run.
	err = kextFile.Close()
	if err != nil {
		log.Warningf("winkext: failed to close kext file: %s", err)
	}

	// Stop and delete the driver.
	err = service.Stop(true)
	if err != nil {
		log.Warningf("winkext: failed to stop kernel service: %s", err)
	}

	err = service.Delete()
	if err != nil {
		log.Warningf("winkext: failed to delete kernel service: %s", err)
	}
	return nil
}

// Sends a shutdown request.
func shutdownRequest() error {
	return kextinterface.SendShutdownCommand(kextFile)
}

// Send request for logs of the kext.
func SendLogRequest() error {
	return kextinterface.SendGetLogsCommand(kextFile)
}

func SendBandwidthStatsRequest() error {
	return kextinterface.SendGetBandwidthStatsCommand(kextFile)
}

func SendPrintMemoryStatsCommand() error {
	return kextinterface.SendPrintMemoryStatsCommand(kextFile)
}

func SendCleanEndedConnection() error {
	return kextinterface.SendCleanEndedConnectionsCommand(kextFile)
}

// RecvVerdictRequest waits for the next verdict request from the kext. If a timeout is reached, both *VerdictRequest and error will be nil.
func RecvVerdictRequest() (*kextinterface.Info, error) {
	return kextinterface.RecvInfo(kextFile)
}

// SetVerdict sets the verdict for a packet and/or connection.
func SetVerdict(pkt *Packet, verdict kextinterface.KextVerdict) error {
	verdictCommand := kextinterface.Verdict{ID: pkt.verdictRequest, Verdict: uint8(verdict)}
	return kextinterface.SendVerdictCommand(kextFile, verdictCommand)
}

// Clears the internal connection cache.
func ClearCache() error {
	return kextinterface.SendClearCacheCommand(kextFile)
}

// Updates a specific connection verdict.
func UpdateVerdict(conn *network.Connection) error {
	if conn.IPVersion == 4 {
		update := kextinterface.UpdateV4{
			Protocol:      conn.Entity.Protocol,
			LocalAddress:  [4]byte(conn.LocalIP),
			LocalPort:     conn.LocalPort,
			RemoteAddress: [4]byte(conn.Entity.IP),
			RemotePort:    conn.Entity.Port,
			Verdict:       uint8(getKextVerdictFromConnection(conn)),
		}

		return kextinterface.SendUpdateV4Command(kextFile, update)
	} else if conn.IPVersion == 6 {
		update := kextinterface.UpdateV6{
			Protocol:      conn.Entity.Protocol,
			LocalAddress:  [16]byte(conn.LocalIP),
			LocalPort:     conn.LocalPort,
			RemoteAddress: [16]byte(conn.Entity.IP),
			RemotePort:    conn.Entity.Port,
			Verdict:       uint8(getKextVerdictFromConnection(conn)),
		}

		return kextinterface.SendUpdateV6Command(kextFile, update)
	}
	return nil
}

func getKextVerdictFromConnection(conn *network.Connection) kextinterface.KextVerdict {
	switch conn.Verdict {
	case network.VerdictUndecided:
		return kextinterface.VerdictUndecided
	case network.VerdictUndeterminable:
		return kextinterface.VerdictUndeterminable
	case network.VerdictAccept:
		if conn.VerdictPermanent {
			return kextinterface.VerdictPermanentAccept
		} else {
			return kextinterface.VerdictAccept
		}
	case network.VerdictBlock:
		if conn.VerdictPermanent {
			return kextinterface.VerdictPermanentBlock
		} else {
			return kextinterface.VerdictBlock
		}
	case network.VerdictDrop:
		if conn.VerdictPermanent {
			return kextinterface.VerdictPermanentDrop
		} else {
			return kextinterface.VerdictDrop
		}
	case network.VerdictRerouteToNameserver:
		return kextinterface.VerdictRerouteToNameserver
	case network.VerdictRerouteToTunnel:
		return kextinterface.VerdictRerouteToTunnel
	case network.VerdictFailed:
		return kextinterface.VerdictFailed
	}
	return kextinterface.VerdictUndeterminable
}

// Returns the kext version.
func GetVersion() (*VersionInfo, error) {
	data, err := kextinterface.ReadVersion(kextFile)
	if err != nil {
		return nil, err
	}

	version := &VersionInfo{
		Major:    data[0],
		Minor:    data[1],
		Revision: data[2],
		Build:    data[3],
	}
	return version, nil
}
