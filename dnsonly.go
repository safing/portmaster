package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/Safing/portbase/info"
	"github.com/Safing/portbase/log"
	"github.com/Safing/portbase/modules"

	// include packages here
	_ "github.com/Safing/portmaster/nameserver/only"
)

func main() {

	// Set Info
	info.Set("Portmaster (DNS only)", "0.2.0")

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
		modules.Shutdown()
	case <-modules.ShuttingDown():
	}

}
