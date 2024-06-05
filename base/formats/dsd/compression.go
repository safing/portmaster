package dsd

import (
	"bytes"
	"compress/gzip"
	"errors"

	"github.com/safing/portmaster/base/formats/varint"
)

// DumpAndCompress stores the interface as a dsd formatted data structure and compresses the resulting data.
func DumpAndCompress(t interface{}, format uint8, compression uint8) ([]byte, error) {
	// Check if compression format is valid.
	compression, ok := ValidateCompressionFormat(compression)
	if !ok {
		return nil, ErrIncompatibleFormat
	}

	// Dump the given data with the given format.
	data, err := Dump(t, format)
	if err != nil {
		return nil, err
	}

	// prepare writer
	packetFormat := varint.Pack8(compression)
	buf := bytes.NewBuffer(nil)
	buf.Write(packetFormat)

	// compress
	switch compression {
	case GZIP:
		// create gzip writer
		gzipWriter, err := gzip.NewWriterLevel(buf, gzip.BestCompression)
		if err != nil {
			return nil, err
		}

		// write data
		n, err := gzipWriter.Write(data)
		if err != nil {
			return nil, err
		}
		if n != len(data) {
			return nil, errors.New("failed to fully write to gzip compressor")
		}

		// flush and write gzip footer
		err = gzipWriter.Close()
		if err != nil {
			return nil, err
		}
	default:
		return nil, ErrIncompatibleFormat
	}

	return buf.Bytes(), nil
}

// DecompressAndLoad decompresses the data using the specified compression format and then loads the resulting data blob into the interface.
func DecompressAndLoad(data []byte, compression uint8, t interface{}) (format uint8, err error) {
	// Check if compression format is valid.
	_, ok := ValidateCompressionFormat(compression)
	if !ok {
		return 0, ErrIncompatibleFormat
	}

	// prepare reader
	buf := bytes.NewBuffer(nil)

	// decompress
	switch compression {
	case GZIP:
		// create gzip reader
		gzipReader, err := gzip.NewReader(bytes.NewBuffer(data))
		if err != nil {
			return 0, err
		}

		// read uncompressed data
		_, err = buf.ReadFrom(gzipReader)
		if err != nil {
			return 0, err
		}

		// flush and verify gzip footer
		err = gzipReader.Close()
		if err != nil {
			return 0, err
		}
	default:
		return 0, ErrIncompatibleFormat
	}

	// assign decompressed data
	data = buf.Bytes()

	format, read, err := loadFormat(data)
	if err != nil {
		return 0, err
	}
	return format, LoadAsFormat(data[read:], format, t)
}
