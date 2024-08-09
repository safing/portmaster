package container

import (
	"errors"
	"io"

	"github.com/safing/structures/varint"
)

// Container is []byte sclie on steroids, allowing for quick data appending, prepending and fetching.
type Container struct {
	compartments [][]byte
	offset       int
	err          error
}

// Data Handling

// NewContainer is DEPRECATED, please use New(), it's the same thing.
func NewContainer(data ...[]byte) *Container {
	return &Container{
		compartments: data,
	}
}

// New creates a new container with an optional initial []byte slice. Data will NOT be copied.
func New(data ...[]byte) *Container {
	return &Container{
		compartments: data,
	}
}

// Prepend prepends data. Data will NOT be copied.
func (c *Container) Prepend(data []byte) {
	if c.offset < 1 {
		c.renewCompartments()
	}
	c.offset--
	c.compartments[c.offset] = data
}

// Append appends the given data. Data will NOT be copied.
func (c *Container) Append(data []byte) {
	c.compartments = append(c.compartments, data)
}

// PrependNumber prepends a number (varint encoded).
func (c *Container) PrependNumber(n uint64) {
	c.Prepend(varint.Pack64(n))
}

// AppendNumber appends a number (varint encoded).
func (c *Container) AppendNumber(n uint64) {
	c.compartments = append(c.compartments, varint.Pack64(n))
}

// PrependInt prepends an int (varint encoded).
func (c *Container) PrependInt(n int) {
	c.Prepend(varint.Pack64(uint64(n)))
}

// AppendInt appends an int (varint encoded).
func (c *Container) AppendInt(n int) {
	c.compartments = append(c.compartments, varint.Pack64(uint64(n)))
}

// AppendAsBlock appends the length of the data and the data itself. Data will NOT be copied.
func (c *Container) AppendAsBlock(data []byte) {
	c.AppendNumber(uint64(len(data)))
	c.Append(data)
}

// PrependAsBlock prepends the length of the data and the data itself. Data will NOT be copied.
func (c *Container) PrependAsBlock(data []byte) {
	c.Prepend(data)
	c.PrependNumber(uint64(len(data)))
}

// AppendContainer appends another Container. Data will NOT be copied.
func (c *Container) AppendContainer(data *Container) {
	c.compartments = append(c.compartments, data.compartments...)
}

// AppendContainerAsBlock appends another Container (length and data). Data will NOT be copied.
func (c *Container) AppendContainerAsBlock(data *Container) {
	c.AppendNumber(uint64(data.Length()))
	c.compartments = append(c.compartments, data.compartments...)
}

// HoldsData returns true if the Container holds any data.
func (c *Container) HoldsData() bool {
	for i := c.offset; i < len(c.compartments); i++ {
		if len(c.compartments[i]) > 0 {
			return true
		}
	}
	return false
}

// Length returns the full length of all bytes held by the container.
func (c *Container) Length() (length int) {
	for i := c.offset; i < len(c.compartments); i++ {
		length += len(c.compartments[i])
	}
	return
}

// Replace replaces all held data with a new data slice. Data will NOT be copied.
func (c *Container) Replace(data []byte) {
	c.compartments = [][]byte{data}
}

// CompileData concatenates all bytes held by the container and returns it as one single []byte slice. Data will NOT be copied and is NOT consumed.
func (c *Container) CompileData() []byte {
	if len(c.compartments) != 1 {
		newBuf := make([]byte, c.Length())
		copyBuf := newBuf
		for i := c.offset; i < len(c.compartments); i++ {
			copy(copyBuf, c.compartments[i])
			copyBuf = copyBuf[len(c.compartments[i]):]
		}
		c.compartments = [][]byte{newBuf}
		c.offset = 0
	}
	return c.compartments[0]
}

// Get returns the given amount of bytes. Data MAY be copied and IS consumed.
func (c *Container) Get(n int) ([]byte, error) {
	buf := c.Peek(n)
	if len(buf) < n {
		return nil, errors.New("container: not enough data to return")
	}
	c.skip(len(buf))
	return buf, nil
}

// GetAll returns all data. Data MAY be copied and IS consumed.
func (c *Container) GetAll() []byte {
	// TODO: Improve.
	buf := c.Peek(c.Length())
	c.skip(len(buf))
	return buf
}

// GetAsContainer returns the given amount of bytes in a new container. Data will NOT be copied and IS consumed.
func (c *Container) GetAsContainer(n int) (*Container, error) {
	newC := c.PeekContainer(n)
	if newC == nil {
		return nil, errors.New("container: not enough data to return")
	}
	c.skip(n)
	return newC, nil
}

// GetMax returns as much as possible, but the given amount of bytes at maximum. Data MAY be copied and IS consumed.
func (c *Container) GetMax(n int) []byte {
	buf := c.Peek(n)
	c.skip(len(buf))
	return buf
}

// WriteToSlice copies data to the give slice until it is full, or the container is empty. It returns the bytes written and if the container is now empty. Data IS copied and IS consumed.
func (c *Container) WriteToSlice(slice []byte) (n int, containerEmptied bool) {
	for i := c.offset; i < len(c.compartments); i++ {
		copy(slice, c.compartments[i])
		if len(slice) < len(c.compartments[i]) {
			// only part was copied
			n += len(slice)
			c.compartments[i] = c.compartments[i][len(slice):]
			c.checkOffset()
			return n, false
		}
		// all was copied
		n += len(c.compartments[i])
		slice = slice[len(c.compartments[i]):]
		c.compartments[i] = nil
		c.offset = i + 1
	}
	c.checkOffset()
	return n, true
}

// WriteAllTo writes all the data to the given io.Writer. Data IS NOT copied (but may be by writer) and IS NOT consumed.
func (c *Container) WriteAllTo(writer io.Writer) error {
	for i := c.offset; i < len(c.compartments); i++ {
		written := 0
		for written < len(c.compartments[i]) {
			n, err := writer.Write(c.compartments[i][written:])
			if err != nil {
				return err
			}
			written += n
		}
	}
	return nil
}

func (c *Container) clean() {
	if c.offset > 100 {
		c.renewCompartments()
	}
}

func (c *Container) renewCompartments() {
	baseLength := len(c.compartments) - c.offset + 5
	newCompartments := make([][]byte, baseLength, baseLength+5)
	copy(newCompartments[5:], c.compartments[c.offset:])
	c.compartments = newCompartments
	c.offset = 4
}

func (c *Container) carbonCopy() *Container {
	newC := &Container{
		compartments: make([][]byte, len(c.compartments)),
		offset:       c.offset,
		err:          c.err,
	}
	copy(newC.compartments, c.compartments)
	return newC
}

func (c *Container) checkOffset() {
	if c.offset >= len(c.compartments) {
		c.offset = len(c.compartments) / 2
	}
}

// Block Handling

// PrependLength prepends the current full length of all bytes in the container.
func (c *Container) PrependLength() {
	c.Prepend(varint.Pack64(uint64(c.Length())))
}

// Peek returns the given amount of bytes. Data MAY be copied and IS NOT consumed.
func (c *Container) Peek(n int) []byte {
	// Check requested length.
	if n <= 0 {
		return nil
	}

	// Check if the first slice holds enough data.
	if len(c.compartments[c.offset]) >= n {
		return c.compartments[c.offset][:n]
	}

	// Start gathering data.
	slice := make([]byte, n)
	copySlice := slice
	n = 0
	for i := c.offset; i < len(c.compartments); i++ {
		copy(copySlice, c.compartments[i])
		if len(copySlice) <= len(c.compartments[i]) {
			n += len(copySlice)
			return slice[:n]
		}
		n += len(c.compartments[i])
		copySlice = copySlice[len(c.compartments[i]):]
	}
	return slice[:n]
}

// PeekContainer returns the given amount of bytes in a new container. Data will NOT be copied and IS NOT consumed.
func (c *Container) PeekContainer(n int) (newC *Container) {
	// Check requested length.
	if n < 0 {
		return nil
	} else if n == 0 {
		return &Container{}
	}

	newC = &Container{}
	for i := c.offset; i < len(c.compartments); i++ {
		if n >= len(c.compartments[i]) {
			newC.compartments = append(newC.compartments, c.compartments[i])
			n -= len(c.compartments[i])
		} else {
			newC.compartments = append(newC.compartments, c.compartments[i][:n])
			n = 0
		}
	}
	if n > 0 {
		return nil
	}
	return newC
}

func (c *Container) skip(n int) {
	for i := c.offset; i < len(c.compartments); i++ {
		if len(c.compartments[i]) <= n {
			n -= len(c.compartments[i])
			c.offset = i + 1
			c.compartments[i] = nil
			if n == 0 {
				c.checkOffset()
				return
			}
		} else {
			c.compartments[i] = c.compartments[i][n:]
			c.checkOffset()
			return
		}
	}
	c.checkOffset()
}

// GetNextBlock returns the next block of data defined by a varint. Data MAY be copied and IS consumed.
func (c *Container) GetNextBlock() ([]byte, error) {
	blockSize, err := c.GetNextN64()
	if err != nil {
		return nil, err
	}
	return c.Get(int(blockSize))
}

// GetNextBlockAsContainer returns the next block of data as a Container defined by a varint. Data will NOT be copied and IS consumed.
func (c *Container) GetNextBlockAsContainer() (*Container, error) {
	blockSize, err := c.GetNextN64()
	if err != nil {
		return nil, err
	}
	return c.GetAsContainer(int(blockSize))
}

// GetNextN8 parses and returns a varint of type uint8.
func (c *Container) GetNextN8() (uint8, error) {
	buf := c.Peek(2)
	num, n, err := varint.Unpack8(buf)
	if err != nil {
		return 0, err
	}
	c.skip(n)
	return num, nil
}

// GetNextN16 parses and returns a varint of type uint16.
func (c *Container) GetNextN16() (uint16, error) {
	buf := c.Peek(3)
	num, n, err := varint.Unpack16(buf)
	if err != nil {
		return 0, err
	}
	c.skip(n)
	return num, nil
}

// GetNextN32 parses and returns a varint of type uint32.
func (c *Container) GetNextN32() (uint32, error) {
	buf := c.Peek(5)
	num, n, err := varint.Unpack32(buf)
	if err != nil {
		return 0, err
	}
	c.skip(n)
	return num, nil
}

// GetNextN64 parses and returns a varint of type uint64.
func (c *Container) GetNextN64() (uint64, error) {
	buf := c.Peek(10)
	num, n, err := varint.Unpack64(buf)
	if err != nil {
		return 0, err
	}
	c.skip(n)
	return num, nil
}
