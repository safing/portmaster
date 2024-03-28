//go:build windows

package iphelper

import (
	"fmt"
	"testing"
)

func TestSockets(t *testing.T) {
	connections, listeners, err := GetTCP4Table()
	if err != nil {
		t.Fatal(err)
	}
	fmt.Println("\nTCP 4 connections:")
	for _, connection := range connections {
		fmt.Printf("%+v\n", connection)
	}
	fmt.Println("\nTCP 4 listeners:")
	for _, listener := range listeners {
		fmt.Printf("%+v\n", listener)
	}

	connections, listeners, err = GetTCP6Table()
	if err != nil {
		t.Fatal(err)
	}
	fmt.Println("\nTCP 6 connections:")
	for _, connection := range connections {
		fmt.Printf("%+v\n", connection)
	}
	fmt.Println("\nTCP 6 listeners:")
	for _, listener := range listeners {
		fmt.Printf("%+v\n", listener)
	}

	binds, err := GetUDP4Table()
	if err != nil {
		t.Fatal(err)
	}
	fmt.Println("\nUDP 4 binds:")
	for _, bind := range binds {
		fmt.Printf("%+v\n", bind)
	}

	binds, err = GetUDP6Table()
	if err != nil {
		t.Fatal(err)
	}
	fmt.Println("\nUDP 6 binds:")
	for _, bind := range binds {
		fmt.Printf("%+v\n", bind)
	}
}
