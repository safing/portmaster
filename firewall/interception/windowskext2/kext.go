//go:build windows
// +build windows

package windowskext

import (
	"fmt"

	"github.com/safing/portbase/log"
	"github.com/safing/portmaster/network"
	"github.com/vlabo/portmaster_windows_rust_kext/kext_interface"
	"golang.org/x/sys/windows"
)

// Package errors
var (
	driverPath string

	service  *kext_interface.KextService
	kextFile *kext_interface.KextFile
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
	service, err = kext_interface.CreateKextService(driverName, driverPath)
	if err != nil {
		return fmt.Errorf("failed to create service: %w", err)
	}

	// Start service and open file
	service.Start(true)
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
	return kext_interface.SendShutdownCommand(kextFile)
}

// Send request for logs of the kext.
func SendLogRequest() error {
	return kext_interface.SendGetLogsCommand(kextFile)
}

func SendBandwidthStatsRequest() error {
	return kext_interface.SendGetBandwidthStatsCommand(kextFile)
}

// RecvVerdictRequest waits for the next verdict request from the kext. If a timeout is reached, both *VerdictRequest and error will be nil.
func RecvVerdictRequest() (*kext_interface.Info, error) {
	return kext_interface.RecvInfo(kextFile)
}

// SetVerdict sets the verdict for a packet and/or connection.
func SetVerdict(pkt *Packet, verdict network.Verdict) error {
	verdictCommand := kext_interface.Verdict{Id: pkt.verdictRequest, Verdict: uint8(verdict)}
	return kext_interface.SendVerdictCommand(kextFile, verdictCommand)
}

// Clears the internal connection cache.
func ClearCache() error {
	return kext_interface.SendClearCacheCommand(kextFile)
}

// Updates a specific connection verdict.
func UpdateVerdict(conn *network.Connection) error {

	update := kext_interface.UpdateV4{
		Protocol:      conn.Entity.Protocol,
		LocalAddress:  [4]byte(conn.LocalIP),
		LocalPort:     conn.LocalPort,
		RemoteAddress: [4]byte(conn.Entity.IP),
		RemotePort:    conn.Entity.Port,
		Verdict:       uint8(conn.Verdict.Active),
	}

	kext_interface.SendUpdateV4Command(kextFile, update)
	return nil
}

// Returns the kext version.
func GetVersion() (*VersionInfo, error) {
	data, err := kext_interface.ReadVersion(kextFile)
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
