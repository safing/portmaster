package netutils

import (
	"errors"
	"os"
)

// IsLocalBindError reports whether err is a failure to bind the local address
// of a socket. Nothing left the host in that case: the local port was not
// available, e.g. because it is in use or reserved by the OS.
func IsLocalBindError(err error) bool {
	var syscallErr *os.SyscallError
	return errors.As(err, &syscallErr) && syscallErr.Syscall == "bind"
}
