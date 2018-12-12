package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"runtime/pprof"
	"syscall"
	"time"

	"github.com/Safing/portbase/info"
	"github.com/Safing/portbase/log"
	"github.com/Safing/portbase/modules"

	// include packages here

	_ "github.com/Safing/portbase/api"
	_ "github.com/Safing/portbase/database/dbmodule"
	_ "github.com/Safing/portbase/database/storage/badger"
	_ "github.com/Safing/portmaster/firewall"
	_ "github.com/Safing/portmaster/nameserver"
)

var (
	printStackOnExit bool
)

func init() {
	flag.BoolVar(&printStackOnExit, "print-stack-on-exit", false, "prints the stack before of shutting down")
}

func main() {

	// Set Info
	info.Set("Portmaster", "0.2.0")

	// Start
	err := modules.Start()
	if err != nil {
		if err == modules.ErrCleanExit {
			os.Exit(0)
		} else {
			err = modules.Shutdown()
			if err != nil {
				log.Shutdown()
			}
			os.Exit(1)
		}
	}

	// Shutdown
	// catch interrupt for clean shutdown
	signalCh := make(chan os.Signal)
	signal.Notify(
		signalCh,
		os.Interrupt,
		syscall.SIGHUP,
		syscall.SIGINT,
		syscall.SIGTERM,
		syscall.SIGQUIT,
	)
	select {
	case <-signalCh:
		fmt.Println(" <INTERRUPT>")
		log.Warning("main: program was interrupted, shutting down.")

		if printStackOnExit {
			fmt.Println("=== PRINTING STACK ===")
			pprof.Lookup("goroutine").WriteTo(os.Stdout, 1)
			fmt.Println("=== END STACK ===")
		}

		go func() {
			modules.Shutdown()
			os.Exit(0)
		}()
		go func() {
			time.Sleep(3 * time.Second)
			fmt.Println("===== TAKING TOO LONG FOR SHUTDOWN - PRINTING STACK TRACES =====")
			pprof.Lookup("goroutine").WriteTo(os.Stdout, 2)
			os.Exit(1)
		}()

	case <-modules.ShuttingDown():
	}

}
