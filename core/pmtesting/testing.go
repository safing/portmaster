// package coretest provides a simple unit test setup routine.
//
// Just include `_ "github.com/safing/portmaster/core/pmtesting"`
//
package pmtesting

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime/pprof"
	"testing"

	"github.com/safing/portbase/dataroot"
	"github.com/safing/portbase/log"
	"github.com/safing/portbase/modules"
	"github.com/safing/portmaster/core"

	// module dependencies
	_ "github.com/safing/portbase/database/storage/hashmap"
)

var (
	printStackOnExit bool
)

func init() {
	flag.BoolVar(&printStackOnExit, "print-stack-on-exit", false, "prints the stack before of shutting down")
}

func TestMain(m *testing.M) {
	// switch databases to memory only
	core.DefaultDatabaseStorageType = "hashmap"

	// set log level
	log.SetLogLevel(log.TraceLevel)

	// tmp dir for data root (db & config)
	tmpDir := filepath.Join(os.TempDir(), "portmaster-testing")
	// initialize data dir
	err := dataroot.Initialize(tmpDir, 0755)
	// start modules
	if err == nil {
		err = modules.Start()
	}
	// handle setup error
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to setup test: %s\n", err)
		printStack()
		os.Exit(1)
	}

	// run tests
	exitCode := m.Run()

	// shutdown
	_ = modules.Shutdown()
	if modules.GetExitStatusCode() != 0 {
		exitCode = modules.GetExitStatusCode()
		fmt.Fprintf(os.Stderr, "failed to cleanly shutdown test: %s\n", err)
	}
	printStack()

	// clean up and exit
	// keep! os.RemoveAll(tmpDir)
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
