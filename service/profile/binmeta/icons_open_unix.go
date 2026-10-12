//go:build !windows

package binmeta

import (
	"os"
	"syscall"
)

// openIconFile opens path read-only without blocking, so that a FIFO in
// place of an icon file does not stall the caller: the type check in
// readIconFile then rejects it on the open handle. O_NONBLOCK has no effect
// on reads from a regular file.
func openIconFile(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
}
