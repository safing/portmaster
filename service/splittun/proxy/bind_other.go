//go:build !linux

package proxy

import "net"

// applyBindToDevice is a no-op on non-Linux platforms; SO_BINDTODEVICE is a
// Linux-specific socket option and has no equivalent here.
func applyBindToDevice(_ *net.Dialer, _ string) {}
