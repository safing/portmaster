package varint

import (
	"encoding/binary"
	"errors"
)

// ErrBufTooSmall is returned when there is not enough data for parsing a varint.
var ErrBufTooSmall = errors.New("varint: buf too small")

// Pack8 packs a uint8 into a VarInt.
func Pack8(n uint8) []byte {
	if n < 128 {
		return []byte{n}
	}
	return []byte{n, 0x01}
}

// Pack16 packs a uint16 into a VarInt.
func Pack16(n uint16) []byte {
	buf := make([]byte, 3)
	w := binary.PutUvarint(buf, uint64(n))
	return buf[:w]
}

// Pack32 packs a uint32 into a VarInt.
func Pack32(n uint32) []byte {
	buf := make([]byte, 5)
	w := binary.PutUvarint(buf, uint64(n))
	return buf[:w]
}

// Pack64 packs a uint64 into a VarInt.
func Pack64(n uint64) []byte {
	buf := make([]byte, 10)
	w := binary.PutUvarint(buf, n)
	return buf[:w]
}

// Unpack8 unpacks a VarInt into a uint8. It returns the extracted int, how many bytes were used and an error.
func Unpack8(blob []byte) (uint8, int, error) {
	if len(blob) < 1 {
		return 0, 0, ErrBufTooSmall
	}
	if blob[0] < 128 {
		return blob[0], 1, nil
	}
	if len(blob) < 2 {
		return 0, 0, ErrBufTooSmall
	}
	if blob[1] != 0x01 {
		return 0, 0, errors.New("varint: encoded integer greater than 255 (uint8)")
	}
	return blob[0], 1, nil
}

// Unpack16 unpacks a VarInt into a uint16. It returns the extracted int, how many bytes were used and an error.
func Unpack16(blob []byte) (uint16, int, error) {
	n, r := binary.Uvarint(blob)
	if r == 0 {
		return 0, 0, ErrBufTooSmall
	}
	if r < 0 {
		return 0, 0, errors.New("varint: encoded integer greater than 18446744073709551615 (uint64)")
	}
	if n > 65535 {
		return 0, 0, errors.New("varint: encoded integer greater than 65535 (uint16)")
	}
	return uint16(n), r, nil
}

// Unpack32 unpacks a VarInt into a uint32. It returns the extracted int, how many bytes were used and an error.
func Unpack32(blob []byte) (uint32, int, error) {
	n, r := binary.Uvarint(blob)
	if r == 0 {
		return 0, 0, ErrBufTooSmall
	}
	if r < 0 {
		return 0, 0, errors.New("varint: encoded integer greater than 18446744073709551615 (uint64)")
	}
	if n > 4294967295 {
		return 0, 0, errors.New("varint: encoded integer greater than 4294967295 (uint32)")
	}
	return uint32(n), r, nil
}

// Unpack64 unpacks a VarInt into a uint64. It returns the extracted int, how many bytes were used and an error.
func Unpack64(blob []byte) (uint64, int, error) {
	n, r := binary.Uvarint(blob)
	if r == 0 {
		return 0, 0, ErrBufTooSmall
	}
	if r < 0 {
		return 0, 0, errors.New("varint: encoded integer greater than 18446744073709551615 (uint64)")
	}
	return n, r, nil
}
