package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/safing/portbase/log"
	"github.com/safing/portmaster/firewall/interception/windowskext"
	"github.com/safing/portmaster/network/packet"
)

var (
	packets chan packet.Packet
)

func main() {

	// check parameter count
	if len(os.Args) < 3 {
		fmt.Printf("usage: %s <dll> <sys>", os.Args[0])
		os.Exit(1)
	}

	// check parameters
	for i := 1; i < 3; i++ {
		if _, err := os.Stat(os.Args[i]); err != nil {
			fmt.Printf("could not access %s: %s", os.Args[i], err)
			os.Exit(2)
		}
	}

	// logging
	_ = log.Start()
	log.Info("starting Portmaster Windows Kext Test Program")

	// init
	err := windowskext.Init(os.Args[1], os.Args[2])
	if err != nil {
		panic(err)
	}

	// start
	err = windowskext.Start()
	if err != nil {
		panic(err)
	}

	packets = make(chan packet.Packet, 1000)
	go windowskext.Handler(packets)
	go handlePackets()

	// catch interrupt for clean shutdown
	signalCh := make(chan os.Signal, 1)
	signal.Notify(
		signalCh,
		os.Interrupt,
		syscall.SIGHUP,
		syscall.SIGINT,
		syscall.SIGTERM,
		syscall.SIGQUIT,
	)
	<-signalCh
	fmt.Println(" <INTERRUPT>")
	log.Warning("program was interrupted, shutting down")

	// stop
	err = windowskext.Stop()
	if err != nil {
		panic(err)
	}

	log.Info("shutdown complete")
	log.Shutdown()

	os.Exit(0)
}

func handlePackets() {
	for {
		pkt := <-packets

		if pkt == nil {
			log.Infof("stopped handling packets")
			return
		}

		log.Infof("received packet: %s", pkt)

		data, err := pkt.GetPayload()
		if err != nil {
			log.Errorf("failed to get payload: %s", err)
		} else {
			log.Infof("payload is: %x", data)
		}

		// reroute dns requests to nameserver
		if pkt.IsOutbound() && !pkt.Info().Src.Equal(pkt.Info().Dst) && pkt.Info().DstPort == 53 {
			log.Infof("rerouting %s", pkt)
			err = pkt.RerouteToNameserver()
			if err != nil {
				log.Errorf("failed to reroute: %s", err)
			}
			continue
		}

		// accept all
		log.Infof("accepting %s", pkt)
		err = pkt.PermanentAccept()
		if err != nil {
			log.Errorf("failed to accept: %s", err)
		}

	}
}
