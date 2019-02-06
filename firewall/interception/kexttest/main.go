package main

import (
	"fmt"

	"github.com/Safing/portmaster/firewall/interception/windowskext"
)

func main() {
	kext, err := windowskext.New("./WinDivert.dll")
	if err != nil {
		panic(err)
	}

	vR, err := kext.RecvVerdictRequest()
	if err != nil {
		panic(err)
	}

	fmt.Printf("verdictRequest: %+v", vR)
}
