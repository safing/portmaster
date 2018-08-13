// +build windows

package main

import (
	"fmt"

	"github.com/Safing/safing-core/process/iphelper"
)

func main() {
	iph, err := iphelper.New()
	if err != nil {
		panic(err)
	}

	fmt.Printf("TCP4\n")
	conns, lConns, err := iph.GetTables(iphelper.TCP, iphelper.IPv4)
	if err != nil {
		panic(err)
	}
	fmt.Printf("Connections:\n")
	for _, conn := range conns {
		fmt.Printf("%s\n", conn)
	}
	fmt.Printf("Listeners:\n")
	for _, conn := range lConns {
		fmt.Printf("%s\n", conn)
	}

	fmt.Printf("\nTCP6\n")
	conns, lConns, err = iph.GetTables(iphelper.TCP, iphelper.IPv6)
	if err != nil {
		panic(err)
	}
	fmt.Printf("Connections:\n")
	for _, conn := range conns {
		fmt.Printf("%s\n", conn)
	}
	fmt.Printf("Listeners:\n")
	for _, conn := range lConns {
		fmt.Printf("%s\n", conn)
	}

	fmt.Printf("\nUDP4\n")
	_, lConns, err = iph.GetTables(iphelper.UDP, iphelper.IPv4)
	if err != nil {
		panic(err)
	}
	for _, conn := range lConns {
		fmt.Printf("%s\n", conn)
	}

	fmt.Printf("\nUDP6\n")
	_, lConns, err = iph.GetTables(iphelper.UDP, iphelper.IPv6)
	if err != nil {
		panic(err)
	}
	for _, conn := range lConns {
		fmt.Printf("%s\n", conn)
	}
}
