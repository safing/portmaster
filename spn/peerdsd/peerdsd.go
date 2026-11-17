// Package peerdsd loads dsd data received from other parties.
//
// dsd.Load transparently decompresses gzip compressed data (dsd format "Z")
// into an unbounded buffer before parsing it. gzip compresses up to about
// 1:1030, so a few KB of data received from the network can make dsd.Load
// allocate many MB (or, combined with the YAML decoder, GB). No SPN component
// sends compressed dsd data, so data received from other parties is checked
// to be in a serialization format before it is handed to dsd.Load.
//
// The msgpack decoder allocates declared lengths before reading the data, so
// msgpack data is checked to not declare more data than it holds.
package peerdsd

import (
	"io"

	"github.com/safing/structures/dsd"
	"github.com/safing/structures/varint"
)

// CheckFormat checks that the given dsd data is in a serialization format and
// not compressed. It returns the same errors as dsd.Load for malformed data
// and dsd.ErrIncompatibleFormat for compressed data and unknown formats, which
// dsd.Load rejects too. For msgpack data, it returns ErrLengthExceedsData if
// a declared length exceeds the data.
// Use it on data received from other parties before passing it to a function
// that calls dsd.Load internally, such as jess.LetterFromDSD.
func CheckFormat(data []byte) error {
	format, read, err := varint.Unpack8(data)
	if err != nil {
		return err
	}
	if len(data) <= read {
		return io.ErrUnexpectedEOF
	}
	if _, ok := dsd.ValidateSerializationFormat(format); !ok {
		return dsd.ErrIncompatibleFormat
	}
	if format == dsd.MsgPack {
		return checkMsgPackLengths(data[read:])
	}
	return nil
}

// Load is like dsd.Load, but rejects compressed data and msgpack data that
// declares lengths larger than the data. Use it instead of dsd.Load for data
// received from other parties.
func Load(data []byte, t any) (format uint8, err error) {
	err = CheckFormat(data)
	if err != nil {
		return 0, err
	}
	return dsd.Load(data, t)
}
