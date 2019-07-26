package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"runtime/pprof"
	"syscall"
	"time"

	"github.com/safing/portbase/info"
	"github.com/safing/portbase/log"
	"github.com/safing/portbase/modules"

	// include packages here
	_ "github.com/safing/portmaster/core"
	_ "github.com/safing/portmaster/firewall"
	_ "github.com/safing/portmaster/nameserver"
	_ "github.com/safing/portmaster/ui"
)

var (
	printStackOnExit   bool
	enableInputSignals bool
)

func init() {
	flag.BoolVar(&printStackOnExit, "print-stack-on-exit", false, "prints the stack before of shutting down")
	flag.BoolVar(&enableInputSignals, "input-signals", false, "emulate signals using stdin")
}

func main() {

	// Set Info
	info.Set("Portmaster", "0.3.4", "AGPLv3", true)

	// Start
	err := modules.Start()
	if err != nil {
		if err == modules.ErrCleanExit {
			os.Exit(0)
		} else {
			modules.Shutdown()
			os.Exit(1)
		}
	}

	// Shutdown
	// catch interrupt for clean shutdown
	signalCh := make(chan os.Signal)
	if enableInputSignals {
		go inputSignals(signalCh)
	}
	signal.Notify(
		signalCh,
		os.Interrupt,
		os.Kill,
		syscall.SIGHUP,
		syscall.SIGINT,
		syscall.SIGTERM,
		syscall.SIGQUIT,
	)
	select {
	case <-signalCh:

		fmt.Println(" <INTERRUPT>")
		log.Warning("main: program was interrupted, shutting down.")

		// catch signals during shutdown
		go func() {
			for {
				<-signalCh
				fmt.Println(" <INTERRUPT> again, but already shutting down")
			}
		}()

		if printStackOnExit {
			fmt.Println("=== PRINTING TRACES ===")
			fmt.Println("=== GOROUTINES ===")
			pprof.Lookup("goroutine").WriteTo(os.Stdout, 1)
			fmt.Println("=== BLOCKING ===")
			pprof.Lookup("block").WriteTo(os.Stdout, 1)
			fmt.Println("=== MUTEXES ===")
			pprof.Lookup("mutex").WriteTo(os.Stdout, 1)
			fmt.Println("=== END TRACES ===")
		}

		go func() {
			time.Sleep(10 * time.Second)
			fmt.Fprintln(os.Stderr, "===== TAKING TOO LONG FOR SHUTDOWN - PRINTING STACK TRACES =====")
			pprof.Lookup("goroutine").WriteTo(os.Stderr, 1)
			os.Exit(1)
		}()

		err := modules.Shutdown()
		if err != nil {
			os.Exit(1)
		} else {
			os.Exit(0)
		}

	case <-modules.ShuttingDown():
	}

}

func inputSignals(signalCh chan os.Signal) {
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		switch scanner.Text() {
		case "SIGHUP":
			signalCh <- syscall.SIGHUP
		case "SIGINT":
			signalCh <- syscall.SIGINT
		case "SIGQUIT":
			signalCh <- syscall.SIGQUIT
		case "SIGTERM":
			signalCh <- syscall.SIGTERM
		}
	}
}
