// Package pmtesting provides a simple unit test setup routine.
//
// Usage:
//
//	package name
//
//	import (
//		"testing"
//
//		"github.com/safing/portmaster/core/pmtesting"
//	)
//
//	func TestMain(m *testing.M) {
//		pmtesting.TestMain(m, module)
//	}
package pmtesting

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime/pprof"
	"testing"

	_ "github.com/safing/portbase/database/storage/hashmap"
	"github.com/safing/portbase/dataroot"
	"github.com/safing/portbase/log"
	"github.com/safing/portbase/modules"
	"github.com/safing/portmaster/core/base"
)

var printStackOnExit bool

func init() {
	flag.BoolVar(&printStackOnExit, "print-stack-on-exit", false, "prints the stack before of shutting down")
}

// TestHookFunc describes the functions passed to TestMainWithHooks.
type TestHookFunc func() error

// TestMain provides a simple unit test setup routine.
func TestMain(m *testing.M, module *modules.Module) {
	TestMainWithHooks(m, module, nil, nil)
}

// TestMainWithHooks provides a simple unit test setup routine and calls
// afterStartFn after modules have started and beforeStopFn before modules
// are shutdown.
func TestMainWithHooks(m *testing.M, module *modules.Module, afterStartFn, beforeStopFn TestHookFunc) {
	// Only enable needed modules.
	modules.EnableModuleManagement(nil)

	// Enable this module for testing.
	if module != nil {
		module.Enable()
	}

	// switch databases to memory only
	base.DefaultDatabaseStorageType = "hashmap"

	// switch API to high port
	base.DefaultAPIListenAddress = "127.0.0.1:10817"

	// set log level
	log.SetLogLevel(log.TraceLevel)

	// tmp dir for data root (db & config)
	tmpDir := filepath.Join(os.TempDir(), "portmaster-testing")
	// initialize data dir
	err := dataroot.Initialize(tmpDir, 0o0755)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to initialize data root: %s\n", err)
		os.Exit(1)
	}

	// start modules
	var exitCode int
	err = modules.Start()
	if err != nil {
		// starting failed
		fmt.Fprintf(os.Stderr, "failed to setup test: %s\n", err)
		exitCode = 1
	} else {
		runTests := true
		if afterStartFn != nil {
			if err := afterStartFn(); err != nil {
				fmt.Fprintf(os.Stderr, "failed to run test start hook: %s\n", err)
				runTests = false
				exitCode = 1
			}
		}

		if runTests {
			// run tests
			exitCode = m.Run()
		}
	}

	if beforeStopFn != nil {
		if err := beforeStopFn(); err != nil {
			fmt.Fprintf(os.Stderr, "failed to run test shutdown hook: %s\n", err)
		}
	}

	// shutdown
	_ = modules.Shutdown()
	if modules.GetExitStatusCode() != 0 {
		exitCode = modules.GetExitStatusCode()
		fmt.Fprintf(os.Stderr, "failed to cleanly shutdown test: %s\n", err)
	}
	printStack()

	// clean up and exit

	// Important: Do not remove tmpDir, as it is used as a cache for updates.
	// remove config
	_ = os.Remove(filepath.Join(tmpDir, "config.json"))
	// remove databases
	_ = os.Remove(filepath.Join(tmpDir, "databases.json"))
	_ = os.RemoveAll(filepath.Join(tmpDir, "databases"))

	os.Exit(exitCode)
}

func printStack() {
	if printStackOnExit {
		fmt.Println("=== PRINTING TRACES ===")
		fmt.Println("=== GOROUTINES ===")
		_ = pprof.Lookup("goroutine").WriteTo(os.Stdout, 2)
		fmt.Println("=== BLOCKING ===")
		_ = pprof.Lookup("block").WriteTo(os.Stdout, 2)
		fmt.Println("=== MUTEXES ===")
		_ = pprof.Lookup("mutex").WriteTo(os.Stdout, 2)
		fmt.Println("=== END TRACES ===")
	}
}
