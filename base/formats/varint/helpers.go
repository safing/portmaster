package varint

import "errors"

// PrependLength prepends the varint encoded length of the byte slice to itself.
func PrependLength(data []byte) []byte {
	return append(Pack64(uint64(len(data))), data...)
}

// GetNextBlock extract the integer from the beginning of the given byte slice and returns the remaining bytes, the extracted integer, and whether there was an error.
func GetNextBlock(data []byte) ([]byte, int, error) {
	l, n, err := Unpack64(data)
	if err != nil {
		return nil, 0, err
	}
	length := int(l)
	totalLength := length + n
	if totalLength > len(data) {
		return nil, 0, errors.New("varint: not enough data for given block length")
	}
	return data[n:totalLength], totalLength, nil
}

// EncodedSize returns the size required to varint-encode an uint.
func EncodedSize(n uint64) (size int) {
	switch {
	case n < 1<<7: // < 128
		return 1
	case n < 1<<14: // < 16384
		return 2
	case n < 1<<21: // < 2097152
		return 3
	case n < 1<<28: // < 268435456
		return 4
	case n < 1<<35: // < 34359738368
		return 5
	case n < 1<<42: // < 4398046511104
		return 6
	case n < 1<<49: // < 562949953421312
		return 7
	case n < 1<<56: // < 72057594037927936
		return 8
	case n < 1<<63: // < 9223372036854775808
		return 9
	default:
		return 10
	}
}
