//go:build windows
// +build windows

package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"playground/kext"
	"playground/logger"
)

const (
	defaultDriverName    = "PortmasterKext"
	defaultDriverRelPath = "..\\_out\\PortmasterKext_test.sys"
	readBufferSize       = 64 * 1024
	logPollInterval      = 1 * time.Second
)

// App holds the application state
type App struct {
	mu sync.RWMutex

	// Driver state
	service    *kext.KextService
	file       *kext.KextFile
	driverPath string
	driverName string
	running    atomic.Bool

	// Redirect state
	redirecting atomic.Bool
	redirectIP  net.IP

	// Logging
	appLog      *logger.Logger
	drvLog      *logger.Logger
	connLog     *logger.Logger
	appLogPath  string
	drvLogPath  string
	connLogPath string
	fileLogging atomic.Bool

	// Control
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// Console logging control
	consoleLogging atomic.Bool

	// Terminal log filter
	terminalLogFilter     *regexp.Regexp
	terminalLogFilterLock sync.RWMutex
}

func main() {
	// Parse command line flags
	logToConsole := flag.Bool("console", true, "Log to console")
	appLogFile := flag.String("app-log", "logs/app.log", "Application log file path")
	drvLogFile := flag.String("drv-log", "logs/driver.log", "Driver log file path")
	connLogFile := flag.String("conn-log", "logs/connections.log", "Connection events log file path")
	driverPath := flag.String("driver", "", "Path to driver.sys (default: relative path)")
	flag.Parse()

	// Create logs directory if it doesn't exist
	logsDir := "logs"
	if err := os.MkdirAll(logsDir, 0755); err != nil {
		fmt.Printf("Failed to create logs directory: %v\n", err)
		os.Exit(1)
	}

	// Always log to files (overwrite on start)
	effectiveAppLogFile := *appLogFile
	effectiveDrvLogFile := *drvLogFile
	effectiveConnLogFile := *connLogFile

	// Create loggers (always log to files, overwrite on start)
	appLog, err := logger.New(logger.Config{
		ToConsole: *logToConsole,
		ToFile:    true,
		FilePath:  effectiveAppLogFile,
		Prefix:    "APP",
		Truncate:  true,
	})
	if err != nil {
		fmt.Printf("Failed to create app logger: %v\n", err)
		os.Exit(1)
	}
	defer appLog.Close()

	drvLog, err := logger.New(logger.Config{
		ToConsole: *logToConsole,
		ToFile:    true,
		FilePath:  effectiveDrvLogFile,
		Prefix:    "DRV",
		Truncate:  true,
	})
	if err != nil {
		fmt.Printf("Failed to create driver logger: %v\n", err)
		os.Exit(1)
	}
	defer drvLog.Close()

	connLog, err := logger.New(logger.Config{
		ToConsole: *logToConsole,
		ToFile:    true,
		FilePath:  effectiveConnLogFile,
		Prefix:    "CONN",
		Truncate:  true,
	})
	if err != nil {
		fmt.Printf("Failed to create connection logger: %v\n", err)
		os.Exit(1)
	}
	defer connLog.Close()

	// Resolve driver path
	resolvedDriverPath := *driverPath
	if resolvedDriverPath == "" {
		exe, err := os.Executable()
		if err != nil {
			appLog.Error("Failed to get executable path: %v", err)
			os.Exit(1)
		}
		resolvedDriverPath = filepath.Join(filepath.Dir(exe), defaultDriverRelPath)
	}

	absDriverPath, err := filepath.Abs(resolvedDriverPath)
	if err != nil {
		appLog.Error("Failed to resolve driver path: %v", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())

	app := &App{
		driverPath:  absDriverPath,
		driverName:  defaultDriverName,
		appLog:      appLog,
		drvLog:      drvLog,
		connLog:     connLog,
		appLogPath:  effectiveAppLogFile,
		drvLogPath:  effectiveDrvLogFile,
		connLogPath: effectiveConnLogFile,
		ctx:         ctx,
		cancel:      cancel,
	}
	app.consoleLogging.Store(*logToConsole)
	app.fileLogging.Store(true)

	// Setup Ctrl+C handler
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		app.appLog.Info("Received interrupt signal, shutting down...")
		app.shutdown()
		os.Exit(0)
	}()

	app.appLog.Info("Driver Test Playground started")
	app.appLog.Info("Driver path: %s", app.driverPath)
	app.printHelp()

	// Start command loop
	app.commandLoop()
}

// parseCommandLine parses a command line string respecting quoted arguments
func parseCommandLine(input string) ([]string, error) {
	var parts []string
	var current strings.Builder
	inQuotes := false
	escapeNext := false

	for i, ch := range input {
		if escapeNext {
			current.WriteRune(ch)
			escapeNext = false
			continue
		}

		switch ch {
		case '\\':
			// Check if next char is a quote
			if i+1 < len(input) && input[i+1] == '"' {
				escapeNext = true
			} else {
				current.WriteRune(ch)
			}
		case '"':
			inQuotes = !inQuotes
		case ' ', '\t':
			if inQuotes {
				current.WriteRune(ch)
			} else if current.Len() > 0 {
				parts = append(parts, current.String())
				current.Reset()
			}
		default:
			current.WriteRune(ch)
		}
	}

	if inQuotes {
		return nil, fmt.Errorf("unclosed quote in command")
	}

	if current.Len() > 0 {
		parts = append(parts, current.String())
	}

	return parts, nil
}

func (app *App) printHelp() {
	help := `
Available commands:
  help                      - Show this help message
  status                    - Show current driver status
  start [driver_path]       - Start driver service (optional: specify driver path)
  stop                      - Stop driver service
  redirect-start <IP>       - Start redirecting TCP/UDP traffic through interface IP
  redirect-stop             - Stop traffic redirection
  logs-toggle               - Toggle console logging on/off
  terminal-log-filter <regex> - Set regex filter for terminal logs (empty to clear)
  exit / quit               - Exit the application

Keyboard shortcuts:
  Ctrl+C                    - Stop driver and exit
  ESC                       - Toggle terminal logging
`
	fmt.Println(help)
}

func (app *App) commandLoop() {
	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Print("\n> ")
		input, err := reader.ReadString('\n')
		if err != nil {
			app.appLog.Error("Failed to read input: %v", err)
			continue
		}

		input = strings.TrimSpace(input)
		if input == "" {
			// Empty input (just Enter) toggles console logs - useful when logs are flooding
			app.toggleConsoleLogs()
			continue
		}

		// Parse command with proper quote handling
		parts, err := parseCommandLine(input)
		if err != nil {
			app.appLog.Error("Failed to parse command: %v", err)
			continue
		}
		if len(parts) == 0 {
			continue
		}

		cmd := strings.ToLower(parts[0])
		args := parts[1:]

		switch cmd {
		case "help", "?":
			app.printHelp()
		case "status":
			app.printStatus()
		case "start":
			driverPath := ""
			if len(args) > 0 {
				driverPath = args[0]
			}
			app.startDriver(driverPath)
		case "stop":
			app.stopDriver()
		case "redirect-start":
			if len(args) < 1 {
				app.appLog.Error("Usage: redirect-start <INTERFACE_IP>")
				continue
			}
			app.startRedirect(args[0])
		case "redirect-stop":
			app.stopRedirect()
		case "logs-toggle", "logs":
			app.toggleConsoleLogs()
		case "terminal-log-filter":
			filterStr := ""
			if len(args) > 0 {
				filterStr = strings.Join(args, " ")
			}
			app.setTerminalLogFilter(filterStr)
		case "exit", "quit", "q":
			app.appLog.Info("Exiting...")
			app.shutdown()
			os.Exit(0)
		default:
			app.appLog.Warn("Unknown command: %s. Type 'help' for available commands.", cmd)
		}
	}
}

func (app *App) printStatus() {
	app.mu.RLock()
	defer app.mu.RUnlock()

	fmt.Println("\n=== Driver Status ===")
	fmt.Printf("Driver Path: %s\n", app.driverPath)
	fmt.Printf("Driver Name: %s\n", app.driverName)

	if app.running.Load() {
		fmt.Println("Status: RUNNING")
		if app.service != nil {
			isRunning, err := app.service.IsRunning()
			if err != nil {
				fmt.Printf("Service Status: Error - %v\n", err)
			} else if isRunning {
				fmt.Println("Service Status: Running")
			} else {
				fmt.Println("Service Status: Stopped")
			}
		}
	} else {
		fmt.Println("Status: STOPPED")
	}

	if app.redirecting.Load() {
		fmt.Printf("Redirect: ACTIVE (IP: %s)\n", app.redirectIP.String())
	} else {
		fmt.Println("Redirect: INACTIVE")
	}

	fmt.Printf("Console Logging: %v\n", app.consoleLogging.Load())
	fmt.Println("=====================")
}

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
	service, err := kext.NewKextService(app.driverName, driverPath)
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
		if err := kext.SendShutdownCommand(app.file); err != nil {
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
		_ = app.service.Close()
		app.service = nil
	}

	app.appLog.Info("Driver stopped")
}

func (app *App) startRedirect(ipStr string) {
	if !app.running.Load() {
		app.appLog.Error("Driver is not running. Start the driver first.")
		return
	}

	if app.redirecting.Load() {
		app.appLog.Warn("Redirect is already active. Stop it first.")
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
	app.mu.Unlock()

	app.redirecting.Store(true)
	app.appLog.Info("Redirect started: routing traffic through %s", ipStr)
	app.appLog.Info("Note: All TCP/UDP (non-DNS) connections will use VerdictRerouteToTunnel")
}

func (app *App) stopRedirect() {
	if !app.redirecting.Load() {
		app.appLog.Warn("Redirect is not active")
		return
	}

	app.redirecting.Store(false)
	app.appLog.Info("Redirect stopped")
}

func (app *App) toggleConsoleLogs() {
	current := app.consoleLogging.Load()
	newValue := !current
	app.consoleLogging.Store(newValue)

	app.appLog.SetConsoleOutput(newValue)
	app.drvLog.SetConsoleOutput(newValue)
	app.connLog.SetConsoleOutput(newValue)

	if newValue {
		fmt.Println("Console logging ENABLED (logs continue to files)")
	} else {
		fmt.Println("Console logging DISABLED (logs continue to files)")
		fmt.Printf("  App:         %s\n", app.appLogPath)
		fmt.Printf("  Driver:      %s\n", app.drvLogPath)
		fmt.Printf("  Connections: %s\n", app.connLogPath)
	}
}

func (app *App) setTerminalLogFilter(filterStr string) {
	app.terminalLogFilterLock.Lock()
	defer app.terminalLogFilterLock.Unlock()

	if filterStr == "" {
		app.terminalLogFilter = nil
		// Clear filter on all loggers
		app.appLog.SetConsoleFilter(nil)
		app.drvLog.SetConsoleFilter(nil)
		app.connLog.SetConsoleFilter(nil)
		app.appLog.Info("Terminal log filter cleared")
		return
	}

	// Try to compile as regex first
	regex, err := regexp.Compile(filterStr)
	if err != nil {
		// If regex compilation fails, treat as literal string
		escapedStr := regexp.QuoteMeta(filterStr)
		regex, err = regexp.Compile(escapedStr)
		if err != nil {
			app.appLog.Error("Failed to create filter: %v", err)
			return
		}
		app.appLog.Info("Terminal log filter set to literal string: %s", filterStr)
	} else {
		app.appLog.Info("Terminal log filter set to regex: %s", filterStr)
	}

	app.terminalLogFilter = regex

	// Apply filter to all loggers
	filterFunc := func(msg string) bool {
		return regex.MatchString(msg)
	}
	app.appLog.SetConsoleFilter(filterFunc)
	app.drvLog.SetConsoleFilter(filterFunc)
	app.connLog.SetConsoleFilter(filterFunc)
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

		app.handleInfo(info, file)
	}
}

func (app *App) handleInfo(info *kext.Info, file *kext.KextFile) {
	if info.ConnectionV4 != nil {
		app.handleConnectionV4(info.ConnectionV4, file)
	} else if info.ConnectionV6 != nil {
		app.handleConnectionV6(info.ConnectionV6, file)
	} else if info.ConnectionEndV4 != nil {
		app.handleConnectionEndV4(info.ConnectionEndV4)
	} else if info.ConnectionEndV6 != nil {
		app.handleConnectionEndV6(info.ConnectionEndV6)
	} else if info.LogLine != nil {
		app.handleLogLine(info.LogLine)
	}
}

func (app *App) handleConnectionV4(conn *kext.ConnectionV4, file *kext.KextFile) {
	localIP := net.IP(conn.LocalIP[:])
	remoteIP := net.IP(conn.RemoteIP[:])
	direction := directionString(conn.Direction)
	protocol := protocolString(conn.Protocol)

	// Determine verdict
	verdict := app.determineVerdict(conn.Protocol, conn.RemotePort)

	app.connLog.Info("[V4] ID=%d PID=%d %s %s %s:%d -> %s:%d verdict=%s",
		conn.ID, conn.ProcessID, direction, protocol,
		localIP, conn.LocalPort, remoteIP, conn.RemotePort,
		verdict.String())

	// Send verdict
	if err := kext.SendVerdictCommand(file, conn.ID, verdict); err != nil {
		app.appLog.Error("Failed to send verdict for connection %d: %v", conn.ID, err)
	}
}

func (app *App) handleConnectionV6(conn *kext.ConnectionV6, file *kext.KextFile) {
	localIP := net.IP(conn.LocalIP[:])
	remoteIP := net.IP(conn.RemoteIP[:])
	direction := directionString(conn.Direction)
	protocol := protocolString(conn.Protocol)

	// Determine verdict
	verdict := app.determineVerdict(conn.Protocol, conn.RemotePort)

	app.connLog.Info("[V6] ID=%d PID=%d %s %s [%s]:%d -> [%s]:%d verdict=%s",
		conn.ID, conn.ProcessID, direction, protocol,
		localIP, conn.LocalPort, remoteIP, conn.RemotePort,
		verdict.String())

	// Send verdict
	if err := kext.SendVerdictCommand(file, conn.ID, verdict); err != nil {
		app.appLog.Error("Failed to send verdict for connection %d: %v", conn.ID, err)
	}
}

func (app *App) handleConnectionEndV4(conn *kext.ConnectionEndV4) {
	localIP := net.IP(conn.LocalIP[:])
	remoteIP := net.IP(conn.RemoteIP[:])

	app.connLog.Info("[V4 END] PID=%d %s %s:%d -> %s:%d",
		conn.ProcessID, protocolString(conn.Protocol),
		localIP, conn.LocalPort, remoteIP, conn.RemotePort)
}

func (app *App) handleConnectionEndV6(conn *kext.ConnectionEndV6) {
	localIP := net.IP(conn.LocalIP[:])
	remoteIP := net.IP(conn.RemoteIP[:])

	app.connLog.Info("[V6 END] PID=%d %s [%s]:%d -> [%s]:%d",
		conn.ProcessID, protocolString(conn.Protocol),
		localIP, conn.LocalPort, remoteIP, conn.RemotePort)
}

func (app *App) handleLogLine(log *kext.LogLine) {
	app.drvLog.Info("[%s] %s", kext.SeverityString(log.Severity), log.Line)
}

func (app *App) determineVerdict(protocol byte, remotePort uint16) kext.KextVerdict {
	// DNS traffic (port 53) - always accept
	if remotePort == 53 {
		return kext.VerdictPermanentAccept
	}

	// If redirecting and it's TCP(6) or UDP(17), redirect to tunnel
	if app.redirecting.Load() && (protocol == 6 || protocol == 17) {
		return kext.VerdictRerouteToTunnel
	}

	// Default: PermanentAccept
	return kext.VerdictPermanentAccept
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
			if err := kext.SendGetLogsCommand(file); err != nil {
				if app.running.Load() {
					app.appLog.Error("Failed to request driver logs: %v", err)
				}
			}
		}
	}
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
