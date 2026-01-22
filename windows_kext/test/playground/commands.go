//go:build windows
// +build windows

package main

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strings"
)

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

func (app *App) toggleConsoleLogs() {
	current := app.consoleLogging.Load()
	newValue := !current
	app.consoleLogging.Store(newValue)

	// app.appLog.SetConsoleOutput(newValue) - app log is always console output
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
