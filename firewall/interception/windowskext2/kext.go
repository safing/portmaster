//go:build windows
// +build windows

package windowskext

import (
	"errors"
	"fmt"
	"unsafe"

	"github.com/safing/portbase/log"
	"github.com/safing/portmaster/network"
	"github.com/vlabo/portmaster_windows_rust_kext/kext_interface"
)

// Package errors
var (
	ErrKextNotReady = errors.New("the windows kernel extension (driver) is not ready to accept commands")
	ErrNoPacketID   = errors.New("the packet has no ID, possibly because it was fast-tracked by the kernel extension")

	driverPath string

	service  *kext_interface.KextService
	kextFile *kext_interface.KextFile
)

const (
	driverName = "PortmasterKext"
)

// Init initializes the DLL and the Kext (Kernel Driver).
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

// Stop intercepting.
func Stop() error {
	// Prepare kernel for shutdown
	err := shutdownRequest()
	if err != nil {
		log.Warningf("winkext: shutdown request failed: %s", err)
	}
	// Close the interface to the driver. Driver will continue to run.
	kextFile.Close()

	// Stop and delete the driver.
	service.Stop(true)
	service.Delete()
	return nil
}

func shutdownRequest() error {
	return kext_interface.WriteCommand(kextFile, kext_interface.BuildShutdown())
}

func SendLogRequest() error {
	return kext_interface.WriteCommand(kextFile, kext_interface.BuildGetLogs())
}

// RecvVerdictRequest waits for the next verdict request from the kext. If a timeout is reached, both *VerdictRequest and error will be nil.
func RecvVerdictRequest() (*kext_interface.Info, error) {
	return kext_interface.ReadInfo(kextFile)
}

// SetVerdict sets the verdict for a packet and/or connection.
func SetVerdict(pkt *Packet, verdict network.Verdict) error {
	if verdict == network.VerdictRerouteToNameserver {
		redirect := kext_interface.Redirect{Id: pkt.verdictRequest, RemoteAddress: []uint8{127, 0, 0, 1}, RemotePort: 53}
		command := kext_interface.BuildRedirect(redirect)
		kext_interface.WriteCommand(kextFile, command)
	} else if verdict == network.VerdictRerouteToTunnel {
		redirect := kext_interface.Redirect{Id: pkt.verdictRequest, RemoteAddress: []uint8{192, 168, 122, 196}, RemotePort: 717}
		command := kext_interface.BuildRedirect(redirect)
		kext_interface.WriteCommand(kextFile, command)
	} else {
		verdict := kext_interface.Verdict{Id: pkt.verdictRequest, Verdict: uint8(verdict)}
		command := kext_interface.BuildVerdict(verdict)
		kext_interface.WriteCommand(kextFile, command)
	}
	return nil
}

func ClearCache() error {
	return kext_interface.WriteCommand(kextFile, kext_interface.BuildClearCache())
}

func UpdateVerdict(conn *network.Connection) error {
	redirectAddress := []uint8{}
	redirectPort := 0
	if conn.Verdict.Active == network.VerdictRerouteToNameserver {
		redirectAddress = []uint8{127, 0, 0, 1}
		redirectPort = 53
	}
	if conn.Verdict.Active == network.VerdictRerouteToTunnel {
		redirectAddress = []uint8{192, 168, 122, 196}
		redirectPort = 717
	}

	update := kext_interface.Update{
		Protocol:        conn.Entity.Protocol,
		LocalAddress:    conn.LocalIP,
		LocalPort:       conn.LocalPort,
		RemoteAddress:   conn.Entity.IP,
		RemotePort:      conn.Entity.Port,
		Verdict:         uint8(conn.Verdict.Active),
		RedirectAddress: redirectAddress,
		RedirectPort:    uint16(redirectPort),
	}

	command := kext_interface.BuildUpdate(update)
	kext_interface.WriteCommand(kextFile, command)
	return nil
}

func GetVersion() (*VersionInfo, error) {
	data, err := kext_interface.ReadVersion(kextFile)
	if err != nil {
		return nil, err
	}

	version := &VersionInfo{
		major:    data[0],
		minor:    data[1],
		revision: data[2],
		build:    data[3],
	}
	return version, nil
}

var sizeOfConnectionStat = uint32(unsafe.Sizeof(ConnectionStat{}))

func GetConnectionsStats() ([]ConnectionStat, error) {
	return nil, nil
}
