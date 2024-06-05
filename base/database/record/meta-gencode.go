package record

import (
	"fmt"
)

// GenCodeSize returns the size of the gencode marshalled byte slice.
func (m *Meta) GenCodeSize() (s int) {
	s += 34
	return
}

// GenCodeMarshal gencode marshalls Meta into the given byte array, or a new one if its too small.
func (m *Meta) GenCodeMarshal(buf []byte) ([]byte, error) {
	size := m.GenCodeSize()
	{
		if cap(buf) >= size {
			buf = buf[:size]
		} else {
			buf = make([]byte, size)
		}
	}
	i := uint64(0)

	{

		buf[0+0] = byte(m.Created >> 0)

		buf[1+0] = byte(m.Created >> 8)

		buf[2+0] = byte(m.Created >> 16)

		buf[3+0] = byte(m.Created >> 24)

		buf[4+0] = byte(m.Created >> 32)

		buf[5+0] = byte(m.Created >> 40)

		buf[6+0] = byte(m.Created >> 48)

		buf[7+0] = byte(m.Created >> 56)

	}
	{

		buf[0+8] = byte(m.Modified >> 0)

		buf[1+8] = byte(m.Modified >> 8)

		buf[2+8] = byte(m.Modified >> 16)

		buf[3+8] = byte(m.Modified >> 24)

		buf[4+8] = byte(m.Modified >> 32)

		buf[5+8] = byte(m.Modified >> 40)

		buf[6+8] = byte(m.Modified >> 48)

		buf[7+8] = byte(m.Modified >> 56)

	}
	{

		buf[0+16] = byte(m.Expires >> 0)

		buf[1+16] = byte(m.Expires >> 8)

		buf[2+16] = byte(m.Expires >> 16)

		buf[3+16] = byte(m.Expires >> 24)

		buf[4+16] = byte(m.Expires >> 32)

		buf[5+16] = byte(m.Expires >> 40)

		buf[6+16] = byte(m.Expires >> 48)

		buf[7+16] = byte(m.Expires >> 56)

	}
	{

		buf[0+24] = byte(m.Deleted >> 0)

		buf[1+24] = byte(m.Deleted >> 8)

		buf[2+24] = byte(m.Deleted >> 16)

		buf[3+24] = byte(m.Deleted >> 24)

		buf[4+24] = byte(m.Deleted >> 32)

		buf[5+24] = byte(m.Deleted >> 40)

		buf[6+24] = byte(m.Deleted >> 48)

		buf[7+24] = byte(m.Deleted >> 56)

	}
	{
		if m.secret {
			buf[32] = 1
		} else {
			buf[32] = 0
		}
	}
	{
		if m.cronjewel {
			buf[33] = 1
		} else {
			buf[33] = 0
		}
	}
	return buf[:i+34], nil
}

// GenCodeUnmarshal gencode unmarshalls Meta and returns the bytes read.
func (m *Meta) GenCodeUnmarshal(buf []byte) (uint64, error) {
	if len(buf) < m.GenCodeSize() {
		return 0, fmt.Errorf("insufficient data: got %d out of %d bytes", len(buf), m.GenCodeSize())
	}

	i := uint64(0)

	{
		m.Created = 0 | (int64(buf[0+0]) << 0) | (int64(buf[1+0]) << 8) | (int64(buf[2+0]) << 16) | (int64(buf[3+0]) << 24) | (int64(buf[4+0]) << 32) | (int64(buf[5+0]) << 40) | (int64(buf[6+0]) << 48) | (int64(buf[7+0]) << 56)
	}
	{
		m.Modified = 0 | (int64(buf[0+8]) << 0) | (int64(buf[1+8]) << 8) | (int64(buf[2+8]) << 16) | (int64(buf[3+8]) << 24) | (int64(buf[4+8]) << 32) | (int64(buf[5+8]) << 40) | (int64(buf[6+8]) << 48) | (int64(buf[7+8]) << 56)
	}
	{
		m.Expires = 0 | (int64(buf[0+16]) << 0) | (int64(buf[1+16]) << 8) | (int64(buf[2+16]) << 16) | (int64(buf[3+16]) << 24) | (int64(buf[4+16]) << 32) | (int64(buf[5+16]) << 40) | (int64(buf[6+16]) << 48) | (int64(buf[7+16]) << 56)
	}
	{
		m.Deleted = 0 | (int64(buf[0+24]) << 0) | (int64(buf[1+24]) << 8) | (int64(buf[2+24]) << 16) | (int64(buf[3+24]) << 24) | (int64(buf[4+24]) << 32) | (int64(buf[5+24]) << 40) | (int64(buf[6+24]) << 48) | (int64(buf[7+24]) << 56)
	}
	{
		m.secret = buf[32] == 1
	}
	{
		m.cronjewel = buf[33] == 1
	}
	return i + 34, nil
}
