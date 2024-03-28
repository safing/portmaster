package netenv

import (
	"context"
	"fmt"
	"net"
	"os"
	"syscall"
	"unsafe"
)

// Windows specific constants for the WSAIoctl interface.
//nolint:golint,stylecheck
const (
	SIO_RCVALL = syscall.IOC_IN | syscall.IOC_VENDOR | 1

	RCVALL_OFF             = 0
	RCVALL_ON              = 1
	RCVALL_SOCKETLEVELONLY = 2
	RCVALL_IPLEVEL         = 3
)

func newICMPListener(address string) (net.PacketConn, error) {
	// This is an attempt to work around the problem described here:
	// https://github.com/golang/go/issues/38427

	// First, get the correct local interface address, as SIO_RCVALL can't be set on a 0.0.0.0 listeners.
	dialedConn, err := net.Dial("ip4:icmp", address)
	if err != nil {
		return nil, fmt.Errorf("failed to dial: %s", err)
	}
	localAddr := dialedConn.LocalAddr()
	dialedConn.Close()

	// Configure the setup routine in order to extract the socket handle.
	var socketHandle syscall.Handle
	cfg := net.ListenConfig{
		Control: func(network, address string, c syscall.RawConn) error {
			return c.Control(func(s uintptr) {
				socketHandle = syscall.Handle(s)
			})
		},
	}

	// Bind to interface.
	conn, err := cfg.ListenPacket(context.Background(), "ip4:icmp", localAddr.String())
	if err != nil {
		return nil, err
	}

	// Set socket option to receive all packets, such as ICMP error messages.
	// This is somewhat dirty, as there is guarantee that socketHandle is still valid.
	// WARNING: The Windows Firewall might just drop the incoming packets you might want to receive.
	unused := uint32(0) // Documentation states that this is unused, but WSAIoctl fails without it.
	flag := uint32(RCVALL_IPLEVEL)
	size := uint32(unsafe.Sizeof(flag))
	err = syscall.WSAIoctl(socketHandle, SIO_RCVALL, (*byte)(unsafe.Pointer(&flag)), size, nil, 0, &unused, nil, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to set socket to listen to all packests: %s", os.NewSyscallError("WSAIoctl", err))
	}

	return conn, nil
}
