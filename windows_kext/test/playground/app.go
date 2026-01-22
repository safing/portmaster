//go:build windows
// +build windows

package main

import (
	"context"
	"net"
	"regexp"
	"sync"
	"sync/atomic"

	"playground/kext"
	"playground/logger"
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
