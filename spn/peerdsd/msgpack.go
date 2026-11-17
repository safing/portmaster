package peerdsd

import "errors"

// ErrLengthExceedsData is returned by CheckFormat and Load if msgpack data
// declares a length that is larger than the remaining data.
var ErrLengthExceedsData = errors.New("peerdsd: msgpack: declared length exceeds data")

// checkMsgPackLengths checks that none of the lengths declared in the first
// msgpack value of data exceeds the remaining data.
//
// The msgpack decoder allocates the declared length of byte strings and
// slices before it reads them: 12 bytes declaring a 4GB byte string make it
// allocate 4GB, and an array declaring 2^32 pointers makes it allocate 32GB,
// which crashes the process with "fatal error: out of memory".
// Data with such a length is truncated and fails to decode anyway, so
// rejecting it does not change which data is accepted. Everything else (eg.
// invalid codes or other truncated data) is left to the decoder.
func checkMsgPackLengths(data []byte) error {
	pending := 1 // Number of values that still need to be read.
	for pending > 0 && len(data) > 0 {
		code := data[0]
		data = data[1:]
		pending--

		var (
			lenSize   int    // Size of the length field.
			length    uint64 // Declared length.
			isBytes   bool   // Whether the length is in bytes, not in values.
			valueSize uint64 = 1
			fixedSize int    // Size of fixed size data.
		)
		switch {
		case code <= 0x7f, code >= 0xe0, code == 0xc0, code == 0xc2, code == 0xc3:
			// fixint, nil, bool
		case code <= 0x8f: // fixmap
			length, valueSize = uint64(code&0x0f), 2
		case code <= 0x9f: // fixarray
			length = uint64(code & 0x0f)
		case code <= 0xbf: // fixstr
			length, isBytes = uint64(code&0x1f), true
		case code == 0xc4, code == 0xd9: // bin8, str8
			lenSize, isBytes = 1, true
		case code == 0xc5, code == 0xda: // bin16, str16
			lenSize, isBytes = 2, true
		case code == 0xc6, code == 0xdb: // bin32, str32
			lenSize, isBytes = 4, true
		case code >= 0xc7 && code <= 0xc9: // ext8, ext16, ext32
			lenSize, isBytes, fixedSize = 1<<(code-0xc7), true, 1 // Ext type.
		case code == 0xca: // float32
			fixedSize = 4
		case code == 0xcb: // float64
			fixedSize = 8
		case code >= 0xcc && code <= 0xd3: // uint8-64, int8-64
			fixedSize = 1 << ((code - 0xcc) % 4)
		case code >= 0xd4 && code <= 0xd8: // fixext1-16
			fixedSize = 1 + 1<<(code-0xd4)
		case code == 0xdc: // array16
			lenSize = 2
		case code == 0xdd: // array32
			lenSize = 4
		case code == 0xde: // map16
			lenSize, valueSize = 2, 2
		case code == 0xdf: // map32
			lenSize, valueSize = 4, 2
		default: // 0xc1 is never used.
			return nil
		}

		// Read the length field.
		if len(data) < lenSize {
			return nil
		}
		for _, b := range data[:lenSize] {
			length = length<<8 | uint64(b)
		}
		data = data[lenSize:]

		// Check the declared length.
		switch {
		case isBytes:
			// The ext type is read together with the data.
			length += uint64(fixedSize)
			if length > uint64(len(data)) {
				return ErrLengthExceedsData
			}
			data = data[length:]
		case fixedSize > 0:
			if len(data) < fixedSize {
				return nil
			}
			data = data[fixedSize:]
		case length > 0:
			// Every value needs at least one byte.
			length *= valueSize
			if length+uint64(pending) > uint64(len(data)) {
				return ErrLengthExceedsData
			}
			pending += int(length) //nolint:gosec // Bounded by len(data).
		}
	}
	return nil
}
