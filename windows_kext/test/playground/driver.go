//go:build windows
// +build windows

package main

import (
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/safing/portmaster/windows_kext/kextinterface"
)

func (app *App) startDriver(customPath string) {
	if app.running.Load() {
		app.appLog.Warn("Driver is already running")
		return
	}

	driverPath := app.driverPath
	if customPath != "" {
		absPath, err := filepath.Abs(customPath)
		if err != nil {
			app.appLog.Error("Invalid driver path: %v", err)
			return
		}
		driverPath = absPath
	}

	// Check if driver file exists
	if _, err := os.Stat(driverPath); os.IsNotExist(err) {
		app.appLog.Error("Driver file not found: %s", driverPath)
		return
	}

	app.appLog.Info("Starting driver from: %s", driverPath)

	// Create service
	service, err := kextinterface.CreateKextService(app.driverName, driverPath)
	if err != nil {
		app.appLog.Error("Failed to create driver service: %v", err)
		return
	}

	// Start service
	if err := service.Start(true); err != nil {
		app.appLog.Error("Failed to start driver service: %v", err)
		_ = service.Delete()
		return
	}

	app.appLog.Info("Driver service started successfully")

	// Open communication channel
	file, err := service.OpenFile(readBufferSize)
	if err != nil {
		app.appLog.Error("Failed to open driver communication: %v", err)
		_ = service.Stop(true)
		_ = service.Delete()
		return
	}

	app.mu.Lock()
	app.service = service
	app.file = file
	app.driverPath = driverPath
	app.mu.Unlock()

	app.running.Store(true)

	// Start background goroutines
	app.wg.Add(2)
	go app.connectionHandler()
	go app.logPoller()

	app.appLog.Info("Driver initialized and ready")
}

func (app *App) stopDriver() {
	if !app.running.Load() {
		app.appLog.Warn("Driver is not running")
		return
	}

	app.appLog.Info("Stopping driver...")

	// Stop redirect if active
	if app.redirecting.Load() {
		app.stopRedirect()
	}

	app.running.Store(false)

	app.mu.Lock()
	defer app.mu.Unlock()

	// Send shutdown command
	if app.file != nil {
		if err := kextinterface.SendShutdownCommand(app.file); err != nil {
			app.appLog.Warn("Failed to send shutdown command: %v", err)
		}
		if err := app.file.Close(); err != nil {
			app.appLog.Warn("Failed to close driver file: %v", err)
		}
		app.file = nil
	}

	// Stop and delete service
	if app.service != nil {
		if err := app.service.Stop(true); err != nil {
			app.appLog.Warn("Failed to stop driver service: %v", err)
		}
		if err := app.service.Delete(); err != nil {
			app.appLog.Warn("Failed to delete driver service: %v", err)
		}
		app.service = nil
	}

	app.appLog.Info("Driver stopped")
}

func (app *App) startRedirect(ipStr string) {
	if !app.running.Load() {
		app.appLog.Error("Driver is not running. Start the driver first.")
		return
	}

	ip := net.ParseIP(ipStr)
	if ip == nil {
		app.appLog.Error("Invalid IP address: %s", ipStr)
		return
	}

	// Validate it's a valid interface IP
	if ip.To4() == nil && ip.To16() == nil {
		app.appLog.Error("Invalid IP address format: %s", ipStr)
		return
	}

	app.mu.Lock()
	app.redirectIP = ip
	file := app.file
	app.mu.Unlock()

	if file == nil {
		app.appLog.Error("Driver communication channel is not available")
		return
	}

	if app.redirecting.Load() {
		if err := kextinterface.SendDisableSplitTunnelCommand(file); err != nil {
			app.appLog.Error("Failed to request SendDisableSplitTunnelCommand: %v", err)
		} else {
			app.appLog.Info("Sent SendDisableSplitTunnelCommand to driver")
		}
	}

	app.redirecting.Store(true)

	if err := kextinterface.SendEnableSplitTunnelCommand(file); err != nil {
		app.appLog.Error("Failed to request SendEnableSplitTunnelCommand: %v", err)
	} else {
		app.appLog.Info("Sent SendEnableSplitTunnelCommand to driver")
		app.appLog.Info("Redirect started: routing traffic through %s", ipStr)
		app.appLog.Info("Note: All TCP/UDP (non-DNS) connections will use VerdictRerouteToTunnel")
	}
}

func (app *App) stopRedirect() {
	if !app.redirecting.Load() {
		app.appLog.Warn("Redirect is not active")
		return
	}

	app.redirecting.Store(false)

	app.mu.RLock()
	file := app.file
	app.mu.RUnlock()

	if file != nil {
		if err := kextinterface.SendDisableSplitTunnelCommand(file); err != nil {
			app.appLog.Error("Failed to request SendDisableSplitTunnelCommand: %v", err)
		} else {
			app.appLog.Info("Sent SendDisableSplitTunnelCommand to driver")
		}
	}
}

func (app *App) shutdown() {
	app.appLog.Info("Shutting down...")
	app.cancel()

	if app.running.Load() {
		app.stopDriver()
	}

	// Wait for goroutines with timeout
	done := make(chan struct{})
	go func() {
		app.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		app.appLog.Info("Clean shutdown completed")
	case <-time.After(5 * time.Second):
		app.appLog.Warn("Shutdown timeout, forcing exit")
	}
}

func (app *App) logPoller() {
	defer app.wg.Done()

	ticker := time.NewTicker(logPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-app.ctx.Done():
			return
		case <-ticker.C:
			if !app.running.Load() {
				continue
			}

			app.mu.RLock()
			file := app.file
			app.mu.RUnlock()

			if file == nil {
				continue
			}

			// Request logs from driver
			if err := kextinterface.SendGetLogsCommand(file); err != nil {
				if app.running.Load() {
					app.appLog.Error("Failed to request driver logs: %v", err)
				}
			}
		}
	}
}
