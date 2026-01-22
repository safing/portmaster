//go:build windows
// +build windows

package main

import (
	"fmt"
	"net"
	"time"

	"playground/kext"
)

func ipVersion(isIPv6 bool) string {
	if isIPv6 {
		return "V6"
	}
	return "V4"
}

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

		info, err := kext.RecvInfo(file)
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
		case info.Connection != nil:
			app.handleConnection(info.Connection, file)
		case info.ConnectionEnd != nil:
			app.handleConnectionEnd(info.ConnectionEnd)
		case info.LogLine != nil:
			app.drvLog.Info("[%s] %s", kext.SeverityString(info.LogLine.Severity), info.LogLine.Line)
		case info.RedirectionRequest != nil:
			app.handleRedirectionRequest(info.RedirectionRequest, file)
		}
	}
}

func (app *App) handleConnection(conn *kext.Connection, file *kext.KextFile) {
	verdict := app.determineVerdict(conn.Protocol, conn.RemoteIP, conn.RemotePort)

	app.connLog.Info("[%s] ID=%d PID=%d %s %s %s:%d -> %s:%d verdict=%s",
		ipVersion(conn.IsIPv6), conn.ID, conn.ProcessID,
		directionString(conn.Direction), protocolString(conn.Protocol),
		conn.LocalIP, conn.LocalPort, conn.RemoteIP, conn.RemotePort,
		verdict.String())

	if err := kext.SendVerdictCommand(file, conn.ID, verdict); err != nil {
		app.appLog.Error("Failed to send verdict for connection %d: %v", conn.ID, err)
	}
}

func (app *App) handleConnectionEnd(conn *kext.ConnectionEnd) {
	app.connLog.Info("[%s END] PID=%d %s %s:%d -> %s:%d",
		ipVersion(conn.IsIPv6), conn.ProcessID, protocolString(conn.Protocol),
		conn.LocalIP, conn.LocalPort, conn.RemoteIP, conn.RemotePort)
}

func (app *App) handleRedirectionRequest(req *kext.RedirectionRequest, file *kext.KextFile) {
	shouldRedirect, redirectIP := app.determineRedirect(req.Protocol, req.RemotePort)

	if shouldRedirect && redirectIP != nil {
		app.connLog.Info("[REDIRECT %s] ID=%d PID=%d %s %s %s:%d -> %s:%d REDIRECT_TO=%s",
			ipVersion(req.IsIPv6), req.ID, req.ProcessID,
			directionString(req.Direction), protocolString(req.Protocol),
			req.LocalIP, req.LocalPort, req.RemoteIP, req.RemotePort,
			redirectIP.String())
	} else {
		app.connLog.Info("[REDIRECT %s] ID=%d PID=%d %s %s %s:%d -> %s:%d PERMIT",
			ipVersion(req.IsIPv6), req.ID, req.ProcessID,
			directionString(req.Direction), protocolString(req.Protocol),
			req.LocalIP, req.LocalPort, req.RemoteIP, req.RemotePort)
	}

	if err := kext.SendRedirectCommand(file, req.ID, shouldRedirect, redirectIP, req.IsIPv6); err != nil {
		app.appLog.Error("Failed to send redirect command for ID %d: %v", req.ID, err)
	}
}

func (app *App) determineVerdict(protocol byte, remoteIP net.IP, remotePort uint16) kext.KextVerdict {
	// DNS traffic (port 53) - always accept
	if remotePort == 53 {
		return kext.VerdictPermanentAccept
	}
	// Non-TCP/UDP traffic - accept
	if protocol != 6 && protocol != 17 {
		return kext.VerdictPermanentAccept
	}
	// Local traffic - accept
	if remoteIP.IsLoopback() || remoteIP.IsPrivate() {
		return kext.VerdictPermanentAccept
	}
	return kext.VerdictPermanentAccept
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
