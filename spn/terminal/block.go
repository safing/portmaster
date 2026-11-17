package terminal

import (
	"encoding/binary"
	"errors"

	"github.com/safing/structures/container"
	"github.com/safing/structures/varint"
)

// ErrBlockTooBig is returned by GetNextBlock if the block size exceeds the
// data that is available.
var ErrBlockTooBig = errors.New("block size exceeds available data")

// GetNextBlock is like container.GetNextBlock, but checks the block size
// before reading the block.
//
// container.GetNextBlock allocates the received block size before checking
// that the data is there, so a crafted size (eg. 1<<42) in data received from
// the network makes it allocate terabytes, which crashes the process with an
// unrecoverable "out of memory" fatal error. Always use this function for
// data received from other parties.
func GetNextBlock(c *container.Container) ([]byte, error) {
	if c.Length() == 0 {
		return nil, varint.ErrBufTooSmall
	}
	blockSize, n, err := varint.Unpack64(c.Peek(binary.MaxVarintLen64))
	if err != nil {
		return nil, err
	}
	if available := c.Length() - n; available < 0 || blockSize > uint64(available) {
		return nil, ErrBlockTooBig
	}
	return c.GetNextBlock()
}
