//go:build windows
// +build windows

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"playground/logger"
)

const (
	defaultDriverName    = "PortmasterKext"
	defaultDriverRelPath = "..\\_out\\PortmasterKext_test.sys"
	readBufferSize       = 64 * 1024
	logPollInterval      = 50 * time.Millisecond
)

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
