//go:build linux

package proxy

import (
	"net"
	"syscall"
)

// applyBindToDevice configures d to bind all outgoing connections to the named
// network interface via the SO_BINDTODEVICE socket option.  The option is set
// in d.Control, which the net package invokes on the raw file descriptor
// immediately after socket creation and before connect(2), ensuring the kernel
// routes the connection through the specified device regardless of the routing
// table.
//
// If iface is empty, d is left unchanged and no binding is performed.
// d.Control is overwritten; any previously set hook is discarded.
func applyBindToDevice(d *net.Dialer, iface string) {
	if iface == "" {
		return
	}
	d.Control = func(network, address string, c syscall.RawConn) error {
		var innerErr error
		err := c.Control(func(fd uintptr) {
			innerErr = syscall.SetsockoptString(
				int(fd),
				syscall.SOL_SOCKET,
				syscall.SO_BINDTODEVICE,
				iface,
			)
		})
		if err != nil {
			return err
		}
		return innerErr
	}
}
