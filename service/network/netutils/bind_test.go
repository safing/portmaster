package netutils

import (
	"errors"
	"net"
	"os"
	"testing"
)

var errTest = errors.New("test error")

func TestIsLocalBindError(t *testing.T) {
	t.Parallel()

	// Occupy a UDP port, then try to dial from that same port.
	held, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %s", err)
	}
	defer func() { _ = held.Close() }()
	heldAddr, ok := held.LocalAddr().(*net.UDPAddr)
	if !ok {
		t.Fatalf("unexpected local address type %T", held.LocalAddr())
	}
	heldPort := heldAddr.Port

	dialer := &net.Dialer{
		LocalAddr: &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: heldPort},
	}
	conn, err := dialer.Dial("udp4", "127.0.0.1:53")
	if err == nil {
		_ = conn.Close()
		t.Fatalf("expected dialing from occupied port %d to fail", heldPort)
	}
	if !IsLocalBindError(err) {
		t.Errorf("expected a bind error, got: %v", err)
	}

	// Other errors are not bind errors.
	if IsLocalBindError(nil) {
		t.Error("nil must not be a bind error")
	}
	if IsLocalBindError(errTest) {
		t.Error("plain error must not be a bind error")
	}
	connectErr := &net.OpError{
		Op:  "dial",
		Net: "udp",
		Err: &os.SyscallError{Syscall: "connect", Err: errTest},
	}
	if IsLocalBindError(connectErr) {
		t.Error("connect error must not be a bind error")
	}
}
