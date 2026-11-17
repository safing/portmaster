//go:build linux

package proc

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestGetTableFromSourceLargeInodeAndUID(t *testing.T) {
	t.Parallel()

	// Linux inode numbers and UIDs are unsigned 32-bit values and may exceed
	// the int32 range.
	const header = "  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode\n"
	content := header +
		"   0: 0100007F:1F90 0100007F:C350 01 00000000:00000000 00:00000000 00000000 4294967294        0 3000000000 1 0000000000000000 100 0 0 10 0\n" +
		"   1: 00000000:0050 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 4294967295 1 0000000000000000 100 0 0 10 0\n"

	path := filepath.Join(t.TempDir(), "tcp")
	err := os.WriteFile(path, []byte(content), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	connections, listeners, err := getTableFromSource(TCP4, path)
	if err != nil {
		t.Fatal(err)
	}
	if len(connections) != 1 || len(listeners) != 1 {
		t.Fatalf("expected 1 connection and 1 listener, got %d and %d", len(connections), len(listeners))
	}
	if uid, inode := connections[0].GetUIDandInode(); uint32(uid) != 4294967294 || uint32(inode) != 3000000000 { //nolint:gosec // Intentionally checking the unsigned 32-bit values.
		t.Errorf("connection: got uid=%d inode=%d", uint32(uid), uint32(inode)) //nolint:gosec // Intentionally printing the unsigned 32-bit values.
	}
	if _, inode := listeners[0].GetUIDandInode(); uint32(inode) != 4294967295 { //nolint:gosec // Intentionally checking the unsigned 32-bit value.
		t.Errorf("listener: got inode=%d", uint32(inode)) //nolint:gosec // Intentionally printing the unsigned 32-bit value.
	}
}

func TestSockets(t *testing.T) {
	t.Parallel()

	connections, listeners, err := GetTCP4Table()
	if err != nil {
		t.Fatal(err)
	}
	fmt.Println("\nTCP 4 connections:")
	for _, connection := range connections {
		pid := GetPID(connection)
		fmt.Printf("%d: %+v\n", pid, connection)
	}
	fmt.Println("\nTCP 4 listeners:")
	for _, listener := range listeners {
		pid := GetPID(listener)
		fmt.Printf("%d: %+v\n", pid, listener)
	}

	connections, listeners, err = GetTCP6Table()
	if err != nil {
		t.Fatal(err)
	}
	fmt.Println("\nTCP 6 connections:")
	for _, connection := range connections {
		pid := GetPID(connection)
		fmt.Printf("%d: %+v\n", pid, connection)
	}
	fmt.Println("\nTCP 6 listeners:")
	for _, listener := range listeners {
		pid := GetPID(listener)
		fmt.Printf("%d: %+v\n", pid, listener)
	}

	binds, err := GetUDP4Table()
	if err != nil {
		t.Fatal(err)
	}
	fmt.Println("\nUDP 4 binds:")
	for _, bind := range binds {
		pid := GetPID(bind)
		fmt.Printf("%d: %+v\n", pid, bind)
	}

	binds, err = GetUDP6Table()
	if err != nil {
		t.Fatal(err)
	}
	fmt.Println("\nUDP 6 binds:")
	for _, bind := range binds {
		pid := GetPID(bind)
		fmt.Printf("%d: %+v\n", pid, bind)
	}
}
