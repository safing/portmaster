package netenv

import (
	"github.com/safing/portmaster-android/go/app_interface"
	"net"
)

func osGetInterfaceAddrs() ([]net.Addr, error) {
	list, err := app_interface.GetNetworkAddresses()
	if err != nil {
		return nil, err
	}

	var netList []net.Addr
	for _, addr := range list {
		ipNetAddr, err := addr.ToIPNet()
		if err == nil {
			netList = append(netList, ipNetAddr)
		}
	}

	return netList, nil
}

func osGetNetworkInterfaces() ([]app_interface.NetworkInterface, error) {
	return app_interface.GetNetworkInterfaces()
}
