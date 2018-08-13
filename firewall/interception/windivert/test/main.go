package main

import (
	"fmt"
	"os"
	"os/signal"
	"runtime/pprof"
	"syscall"
	"time"

	"github.com/Safing/safing-core/firewall/interception/windivert"
	"github.com/Safing/safing-core/log"
	"github.com/Safing/safing-core/modules"
	"github.com/Safing/safing-core/network/packet"
)

func main() {
	modules.RegisterLogger(log.Logger)

	wd, err := windivert.New("C:/WinDivert.dll", "")
	if err != nil {
		panic(err)
	}
	defer wd.Close()

	packets := make(chan packet.Packet, 1000)
	wd.Packets(packets)
	go func() {
		for pkt := range packets {
			log.Infof("pkt: %s", pkt)
			if pkt.GetIPHeader().Protocol == 0 || pkt.GetIPHeader().Protocol == 128 {
				pl := pkt.GetPayload()
				log.Infof("payload (%d): %s", len(pl), string(pl))
			}
			pkt.Accept()
		}
	}()

	// SHUTDOWN
	// catch interrupt for clean shutdown
	signalCh := make(chan os.Signal)
	signal.Notify(
		signalCh,
		os.Interrupt,
		os.Kill,
		syscall.SIGHUP,
		syscall.SIGINT,
		syscall.SIGTERM,
		syscall.SIGQUIT,
		syscall.SIGKILL,
		syscall.SIGSEGV,
	)
	select {
	case <-signalCh:
		log.Warning("program was interrupted, shutting down.")
		modules.InitiateFullShutdown()
	case <-modules.GlobalShutdown:
	}

	// wait for shutdown to complete, panic after timeout
	time.Sleep(5 * time.Second)
	fmt.Println("===== TAKING TOO LONG FOR SHUTDOWN - PRINTING STACK TRACES =====")
	pprof.Lookup("goroutine").WriteTo(os.Stdout, 1)
	os.Exit(1)

}
