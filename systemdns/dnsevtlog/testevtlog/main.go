package main

import (
	"fmt"
	"os"

	"github.com/safing/portbase/log"
	"github.com/safing/portmaster/systemdns/dnsevtlog"
)

func main() {
	log.SetLogLevel(log.DebugLevel)
	err := log.Start()
	if err != nil {
		fmt.Print(err)
		os.Exit(1)
	}

	sub, err := dnsevtlog.NewSubscription()
	if err != nil {
		fmt.Print(err)
		os.Exit(1)
	}

	sub.ReadWorker()
}
