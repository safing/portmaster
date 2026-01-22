//go:build windows
// +build windows

package kext

import (
	"encoding/binary"
	"io"
	"net"
)

// SendShutdownCommand sends shutdown command to driver
func SendShutdownCommand(w io.Writer) error {
	_, err := w.Write([]byte{CommandShutdown})
	return err
}

// SendVerdictCommand sends a verdict for a connection
func SendVerdictCommand(w io.Writer, id uint64, verdict KextVerdict) error {
	v := Verdict{
		Command: CommandVerdict,
		ID:      id,
		Verdict: uint8(verdict),
	}
	return binary.Write(w, binary.LittleEndian, v)
}

// SendUpdateV4Command sends an IPv4 verdict update
func SendUpdateV4Command(w io.Writer, update UpdateV4) error {
	update.Command = CommandUpdateV4
	return binary.Write(w, binary.LittleEndian, update)
}

// SendUpdateV6Command sends an IPv6 verdict update
func SendUpdateV6Command(w io.Writer, update UpdateV6) error {
	update.Command = CommandUpdateV6
	return binary.Write(w, binary.LittleEndian, update)
}

// SendClearCacheCommand clears the driver cache
func SendClearCacheCommand(w io.Writer) error {
	_, err := w.Write([]byte{CommandClearCache})
	return err
}

// SendGetLogsCommand requests buffered logs from driver
func SendGetLogsCommand(w io.Writer) error {
	_, err := w.Write([]byte{CommandGetLogs})
	return err
}

// SendGetBandwidthStatsCommand requests bandwidth statistics
func SendGetBandwidthStatsCommand(w io.Writer) error {
	_, err := w.Write([]byte{CommandBandwidthStats})
	return err
}

// SendRedirectCommand sends a redirect decision for a connection (works for both V4 and V6)
// If redirect is true, the connection will be redirected through the specified localAddress interface
// If redirect is false, the connection proceeds without modification (permit)
func SendRedirectCommand(w io.Writer, id uint64, redirect bool, localAddress net.IP, isIPv6 bool) error {
	if isIPv6 {
		cmd := RedirectV6{
			Command:  CommandRedirectV6,
			ID:       id,
			Redirect: 0,
		}
		if redirect && localAddress != nil {
			cmd.Redirect = 1
			copy(cmd.LocalAddress[:], localAddress.To16())
		}
		return binary.Write(w, binary.LittleEndian, cmd)
	}

	cmd := RedirectV4{
		Command:  CommandRedirectV4,
		ID:       id,
		Redirect: 0,
	}
	if redirect && localAddress != nil {
		cmd.Redirect = 1
		copy(cmd.LocalAddress[:], localAddress.To4())
	}
	return binary.Write(w, binary.LittleEndian, cmd)
}

// SendEnableSplitTunnelCommand enables split tunneling in the driver
func SendEnableSplitTunnelCommand(w io.Writer) error {
	_, err := w.Write([]byte{CommandEnableSplitTunnel})
	return err
}

// SendDisableSplitTunnelCommand disables split tunneling in the driver
func SendDisableSplitTunnelCommand(w io.Writer) error {
	_, err := w.Write([]byte{CommandDisableSplitTunnel})
	return err
}
