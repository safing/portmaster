package main

import (
	"fmt"
	"time"

	processInfo "github.com/shirou/gopsutil/process"
	"github.com/spf13/cobra"

	"github.com/safing/portmaster/service/network/packet"
	"github.com/safing/portmaster/service/network/socket"
	"github.com/safing/portmaster/service/network/state"
)

func init() {
	rootCmd.AddCommand(netStateCmd)
	netStateCmd.AddCommand(netStateMonitorCmd)
}

var (
	netStateCmd = &cobra.Command{
		Use:   "netstate",
		Short: "Print current network state as received from the system",
		RunE:  netState,
	}
	netStateMonitorCmd = &cobra.Command{
		Use:   "monitor",
		Short: "Monitor the network state and print any new connections",
		RunE:  netStateMonitor,
	}

	seen = make(map[string]bool)
)

func netState(cmd *cobra.Command, args []string) error {
	tables := state.GetInfo()

	for _, s := range tables.TCP4Connections {
		checkAndPrintConnectionInfoIfNew(packet.IPv4, packet.TCP, s)
	}
	for _, s := range tables.TCP4Listeners {
		checkAndPrintBindInfoIfNew(packet.IPv4, packet.TCP, s)
	}
	for _, s := range tables.TCP6Connections {
		checkAndPrintConnectionInfoIfNew(packet.IPv6, packet.TCP, s)
	}
	for _, s := range tables.TCP6Listeners {
		checkAndPrintBindInfoIfNew(packet.IPv6, packet.TCP, s)
	}
	for _, s := range tables.UDP4Binds {
		checkAndPrintBindInfoIfNew(packet.IPv6, packet.UDP, s)
	}
	for _, s := range tables.UDP6Binds {
		checkAndPrintBindInfoIfNew(packet.IPv6, packet.UDP, s)
	}
	return nil
}

func netStateMonitor(cmd *cobra.Command, args []string) error {
	for {
		err := netState(cmd, args)
		if err != nil {
			return err
		}

		time.Sleep(10 * time.Millisecond)
	}
}

func checkAndPrintConnectionInfoIfNew(ipv packet.IPVersion, p packet.IPProtocol, s *socket.ConnectionInfo) {
	// Build connection string.
	c := fmt.Sprintf(
		"%s %s %s:%d <-> %s:%d",
		ipv, p,
		s.Local.IP,
		s.Local.Port,
		s.Remote.IP,
		s.Remote.Port,
	)

	checkAndPrintSocketInfoIfNew(c, s)
}

func checkAndPrintBindInfoIfNew(ipv packet.IPVersion, p packet.IPProtocol, s *socket.BindInfo) {
	// Build connection string.
	c := fmt.Sprintf(
		"%s %s bind %s:%d",
		ipv, p,
		s.Local.IP,
		s.Local.Port,
	)

	checkAndPrintSocketInfoIfNew(c, s)
}

func checkAndPrintSocketInfoIfNew(c string, s socket.Info) {
	// Return if connection was already seen.
	if _, ok := seen[c]; ok {
		return
	}
	// Otherwise, add as seen.
	seen[c] = true

	// Check if we have the PID.
	_, _, err := state.CheckPID(s, false)

	// Print result.
	if err == nil {

		pInfo, err := processInfo.NewProcess(int32(s.GetPID()))

		if err != nil {
			fmt.Printf("%s %d no binary: %s\n", c, s.GetPID(), err)
		} else {
			exe, _ := pInfo.Exe()
			fmt.Printf("%s %d %s\n", c, s.GetPID(), exe)
		}

	} else {
		fmt.Printf("%s %d (err: %s)\n", c, s.GetPID(), err)
	}
}
