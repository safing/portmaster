//go:build windows
// +build windows

package main

import (
	"fmt"
	"net"
	"time"

	"github.com/safing/portmaster/windows_kext/kextinterface"
)

func directionString(d byte) string {
	if d == 0 {
		return "OUT"
	}
	return "IN"
}

func protocolString(p byte) string {
	switch p {
	case 1:
		return "ICMP"
	case 6:
		return "TCP"
	case 17:
		return "UDP"
	case 58:
		return "ICMPv6"
	default:
		return fmt.Sprintf("PROTO-%d", p)
	}
}

// verdictString converts a verdict to a human-readable string
func verdictString(v kextinterface.KextVerdict) string {
	switch v {
	case kextinterface.VerdictUndecided:
		return "Undecided"
	case kextinterface.VerdictUndeterminable:
		return "Undeterminable"
	case kextinterface.VerdictAccept:
		return "Accept"
	case kextinterface.VerdictPermanentAccept:
		return "PermanentAccept"
	case kextinterface.VerdictBlock:
		return "Block"
	case kextinterface.VerdictPermanentBlock:
		return "PermanentBlock"
	case kextinterface.VerdictDrop:
		return "Drop"
	case kextinterface.VerdictPermanentDrop:
		return "PermanentDrop"
	case kextinterface.VerdictRerouteToNameserver:
		return "RerouteToNameserver"
	case kextinterface.VerdictRerouteToTunnel:
		return "RerouteToTunnel"
	case kextinterface.VerdictFailed:
		return "Failed"
	default:
		return fmt.Sprintf("Unknown(%d)", v)
	}
}

// severityString converts a log severity level to a human-readable string
func severityString(s byte) string {
	switch s {
	case 1: // Trace
		return "TRACE"
	case 2: // Debug
		return "DEBUG"
	case 3: // Info
		return "INFO"
	case 4: // Warning
		return "WARN"
	case 5: // Error
		return "ERROR"
	case 6: // Critical
		return "CRIT"
	default:
		return fmt.Sprintf("LVL%d", s)
	}
}

func (app *App) connectionHandler() {
	defer app.wg.Done()

	for app.running.Load() {
		app.mu.RLock()
		file := app.file
		app.mu.RUnlock()

		if file == nil {
			time.Sleep(100 * time.Millisecond)
			continue
		}

		info, err := kextinterface.RecvInfo(file)
		if err != nil {
			if app.running.Load() {
				app.appLog.Error("Failed to receive info: %v", err)
			}
			continue
		}

		if info == nil {
			continue
		}

		switch {
		case info.ConnectionV4 != nil:
			app.handleConnectionV4(info.ConnectionV4, file)
		case info.ConnectionV6 != nil:
			app.handleConnectionV6(info.ConnectionV6, file)
		case info.ConnectionEndV4 != nil:
			app.handleConnectionEndV4(info.ConnectionEndV4)
		case info.ConnectionEndV6 != nil:
			app.handleConnectionEndV6(info.ConnectionEndV6)
		case info.LogLine != nil:
			app.drvLog.Info("[%s] %s", severityString(info.LogLine.Severity), info.LogLine.Line)
		case info.RedirectionRequestV4 != nil:
			app.handleRedirectionRequestV4(info.RedirectionRequestV4, file)
		case info.RedirectionRequestV6 != nil:
			app.handleRedirectionRequestV6(info.RedirectionRequestV6, file)
		}
	}
}

func (app *App) handleConnectionV4(conn *kextinterface.ConnectionV4, file *kextinterface.KextFile) {
	localIP := net.IP(conn.LocalIP[:])
	remoteIP := net.IP(conn.RemoteIP[:])
	verdict := app.determineVerdict(conn.Protocol, remoteIP, conn.RemotePort)

	app.connLog.Info("[V4] ID=%d PID=%d %s %s %s:%d -> %s:%d verdict=%s",
		conn.ID, conn.ProcessID,
		directionString(conn.Direction), protocolString(conn.Protocol),
		localIP, conn.LocalPort, remoteIP, conn.RemotePort,
		verdictString(verdict))

	v := kextinterface.Verdict{ID: conn.ID, Verdict: uint8(verdict)}
	if err := kextinterface.SendVerdictCommand(file, v); err != nil {
		app.appLog.Error("Failed to send verdict for connection %d: %v", conn.ID, err)
	}
}

func (app *App) handleConnectionV6(conn *kextinterface.ConnectionV6, file *kextinterface.KextFile) {
	localIP := net.IP(conn.LocalIP[:])
	remoteIP := net.IP(conn.RemoteIP[:])
	verdict := app.determineVerdict(conn.Protocol, remoteIP, conn.RemotePort)

	app.connLog.Info("[V6] ID=%d PID=%d %s %s %s:%d -> %s:%d verdict=%s",
		conn.ID, conn.ProcessID,
		directionString(conn.Direction), protocolString(conn.Protocol),
		localIP, conn.LocalPort, remoteIP, conn.RemotePort,
		verdictString(verdict))

	v := kextinterface.Verdict{ID: conn.ID, Verdict: uint8(verdict)}
	if err := kextinterface.SendVerdictCommand(file, v); err != nil {
		app.appLog.Error("Failed to send verdict for connection %d: %v", conn.ID, err)
	}
}

func (app *App) handleConnectionEndV4(conn *kextinterface.ConnectionEndV4) {
	localIP := net.IP(conn.LocalIP[:])
	remoteIP := net.IP(conn.RemoteIP[:])
	app.connLog.Info("[V4 END] PID=%d %s %s:%d -> %s:%d",
		conn.ProcessID, protocolString(conn.Protocol),
		localIP, conn.LocalPort, remoteIP, conn.RemotePort)
}

func (app *App) handleConnectionEndV6(conn *kextinterface.ConnectionEndV6) {
	localIP := net.IP(conn.LocalIP[:])
	remoteIP := net.IP(conn.RemoteIP[:])
	app.connLog.Info("[V6 END] PID=%d %s %s:%d -> %s:%d",
		conn.ProcessID, protocolString(conn.Protocol),
		localIP, conn.LocalPort, remoteIP, conn.RemotePort)
}

func (app *App) handleRedirectionRequestV4(req *kextinterface.RedirectionRequestV4, file *kextinterface.KextFile) {
	localIP := net.IP(req.LocalIP[:])
	shouldRedirect, redirectIP := app.determineRedirect(req.Protocol, 0) // Note: RedirectionRequest doesn't have RemotePort

	if shouldRedirect && redirectIP != nil {
		app.connLog.Info("[REDIRECT V4] ID=%d PID=%d %s %s:%d REDIRECT_TO=%s",
			req.ID, req.ProcessID,
			protocolString(req.Protocol),
			localIP, req.LocalPort,
			redirectIP.String())
	} else {
		app.connLog.Info("[REDIRECT V4] ID=%d PID=%d %s %s:%d PERMIT",
			req.ID, req.ProcessID,
			protocolString(req.Protocol),
			localIP, req.LocalPort)
	}

	cmd := kextinterface.RedirectV4{
		ID:       req.ID,
		Redirect: 0,
	}
	if shouldRedirect && redirectIP != nil {
		cmd.Redirect = 1
		copy(cmd.LocalAddress[:], redirectIP.To4())
	}
	if err := kextinterface.SendRedirectV4Command(file, cmd); err != nil {
		app.appLog.Error("Failed to send redirect command for ID %d: %v", req.ID, err)
	}
}

func (app *App) handleRedirectionRequestV6(req *kextinterface.RedirectionRequestV6, file *kextinterface.KextFile) {
	localIP := net.IP(req.LocalIP[:])
	shouldRedirect, redirectIP := app.determineRedirect(req.Protocol, 0) // Note: RedirectionRequest doesn't have RemotePort

	if shouldRedirect && redirectIP != nil {
		app.connLog.Info("[REDIRECT V6] ID=%d PID=%d %s %s:%d REDIRECT_TO=%s",
			req.ID, req.ProcessID,
			protocolString(req.Protocol),
			localIP, req.LocalPort,
			redirectIP.String())
	} else {
		app.connLog.Info("[REDIRECT V6] ID=%d PID=%d %s %s:%d PERMIT",
			req.ID, req.ProcessID,
			protocolString(req.Protocol),
			localIP, req.LocalPort)
	}

	cmd := kextinterface.RedirectV6{
		ID:       req.ID,
		Redirect: 0,
	}
	if shouldRedirect && redirectIP != nil {
		cmd.Redirect = 1
		copy(cmd.LocalAddress[:], redirectIP.To16())
	}
	if err := kextinterface.SendRedirectV6Command(file, cmd); err != nil {
		app.appLog.Error("Failed to send redirect command for ID %d: %v", req.ID, err)
	}
}

func (app *App) determineVerdict(protocol byte, remoteIP net.IP, remotePort uint16) kextinterface.KextVerdict {
	// DNS traffic (port 53) - always accept
	if remotePort == 53 {
		return kextinterface.VerdictPermanentAccept
	}
	// Non-TCP/UDP traffic - accept
	if protocol != 6 && protocol != 17 {
		return kextinterface.VerdictPermanentAccept
	}
	// Local traffic - accept
	if remoteIP.IsLoopback() || remoteIP.IsPrivate() {
		return kextinterface.VerdictPermanentAccept
	}
	return kextinterface.VerdictPermanentAccept
}

// determineRedirect decides whether to redirect a connection and to which IP
func (app *App) determineRedirect(protocol byte, remotePort uint16) (bool, net.IP) {
	// DNS traffic (port 53) - never redirect
	if remotePort == 53 {
		return false, nil
	}
	// Only redirect TCP/UDP when redirecting is enabled
	if app.redirecting.Load() && (protocol == 6 || protocol == 17) {
		app.mu.RLock()
		redirectIP := app.redirectIP
		app.mu.RUnlock()
		if redirectIP != nil {
			return true, redirectIP
		}
	}
	return false, nil
}
