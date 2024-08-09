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

	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/service/firewall/interception/windowskext"
	"github.com/safing/portmaster/service/network/packet"
)

var (
	packets    chan packet.Packet
	shutdownCh = make(chan struct{})

	getPayload      bool
	rerouteDNS      bool
	permanentAccept bool
	maxPackets      int
)

func init() {
	flag.BoolVar(&getPayload, "get-payload", false, "get payload of handled packets")
	flag.BoolVar(&rerouteDNS, "reroute-dns", false, "reroute dns to own IP")
	flag.BoolVar(&permanentAccept, "permanent-accept", false, "permanent-accept packets")
	flag.IntVar(&maxPackets, "max-packets", 0, "handle specified amount of packets, then exit")
}

func main() {
	flag.Parse()

	// check parameter count
	if flag.NArg() != 2 {
		fmt.Printf("usage: %s [options] <dll> <sys>\n", os.Args[0])
		flag.Usage()
		os.Exit(1)
	}

	// logging
	err := log.Start()
	if err != nil {
		fmt.Printf("failed to start logging: %s\n", err)
		os.Exit(1)
	}
	defer log.Shutdown()
	log.SetLogLevel(log.TraceLevel)
	log.Info("starting windows kext test program")

	// Check paths.
	dllPath, err := filepath.Abs(flag.Arg(0))
	if err == nil {
		_, err = os.Stat(dllPath)
	}
	if err != nil {
		log.Criticalf("cannot find .dll: %s\n", err)
		return
	}
	log.Infof("using .dll at %s", dllPath)

	sysPath, err := filepath.Abs(flag.Arg(1))
	if err == nil {
		_, err = os.Stat(sysPath)
	}
	if err != nil {
		log.Criticalf("cannot find .sys: %s", err)
		return
	}
	log.Infof("using .sys at %s", sysPath)

	// init
	err = windowskext.Init(sysPath)
	if err != nil {
		log.Criticalf("failed to init kext: %s", err)
		return
	}

	// start
	err = windowskext.Start()
	if err != nil {
		log.Criticalf("failed to start kext: %s", err)
		return
	}

	packets = make(chan packet.Packet, 1000)
	go windowskext.Handler(context.TODO(), packets)
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
	select {
	case <-signalCh:
		fmt.Println(" <INTERRUPT>")
		log.Warning("program was interrupted, shutting down")
	case <-shutdownCh:
		log.Warningf("shutting down")
	}

	// stop
	err = windowskext.Stop()
	if err != nil {
		log.Criticalf("failed to stop kext: %s", err)
	}

	log.Info("shutdown complete")
}

func handlePackets() {
	var err error
	var handledPackets int

	for {
		pkt := <-packets

		if pkt == nil {
			log.Infof("stopped handling packets")
			return
		}

		log.Infof("received packet: %s", pkt)
		handledPackets++

		if getPayload {
			data := pkt.Payload()
			log.Infof("payload is: %x", data)
		}

		// reroute dns requests to nameserver
		if rerouteDNS {
			if pkt.IsOutbound() && !pkt.Info().Src.Equal(pkt.Info().Dst) && pkt.Info().DstPort == 53 {
				log.Infof("rerouting %s", pkt)
				err = pkt.RerouteToNameserver()
				if err != nil {
					log.Errorf("failed to reroute: %s", err)
				}
				continue
			}
		}

		// accept all
		log.Infof("accepting %s", pkt)
		if permanentAccept {
			err = pkt.PermanentAccept()
		} else {
			err = pkt.Accept()
		}
		if err != nil {
			log.Errorf("failed to accept: %s", err)
		}

		if maxPackets > 0 && handledPackets > maxPackets {
			log.Infof("max-packets (%d) reached", maxPackets)
			close(shutdownCh)
			return
		}

	}
}
