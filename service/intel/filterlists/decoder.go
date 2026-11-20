package filterlists

import (
	"bufio"
	"compress/gzip"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"github.com/safing/portmaster/base/utils"
	"github.com/safing/structures/dsd"
)

type listEntry struct {
	Type      string          `json:"type"`
	Entity    string          `json:"entity"`
	Whitelist bool            `json:"whitelist"`
	Resources []entryResource `json:"resources"`
}

type entryResource struct {
	SourceID   string `json:"sourceID"`
	ResourceID string `json:"resourceID"`
}

func (entry *listEntry) getSources() (sourceIDs []string) {
	sourceIDs = make([]string, 0, len(entry.Resources))

	for _, resource := range entry.Resources {
		if !utils.StringInSlice(sourceIDs, resource.SourceID) {
			sourceIDs = append(sourceIDs, resource.SourceID)
		}
	}

	return
}

// decodeFile decodes a DSDL filterlists file and sends decoded entities to
// ch. It blocks until all list entries have been consumed or ctx is cancelled.
func decodeFile(ctx context.Context, r io.Reader, ch chan<- *listEntry) error {
	compressed, format, err := parseHeader(r)
	if err != nil {
		return fmt.Errorf("failed to parser header: %w", err)
	}

	if compressed {
		r, err = gzip.NewReader(r)
		if err != nil {
			return fmt.Errorf("failed to open gzip reader: %w", err)
		}
	}

	// Buffer the reads: the entry lengths are read byte by byte, which would
	// otherwise be one read from the (decompressing) source per byte.
	reader := bufio.NewReaderSize(r, 64*1024)
	// The entry buffer is reused for all entries. This is safe, as all
	// formats that can decode a listEntry copy the strings out of the data.
	var buf []byte
	var entryCount int
	for {
		entryCount++
		length, readErr := binary.ReadUvarint(reader)
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return nil
			}
			return fmt.Errorf("failed to load varint entity length: %w", readErr)
		}

		if uint64(cap(buf)) < length {
			buf = make([]byte, length)
		}
		blob := buf[:length]
		_, readErr = io.ReadFull(reader, blob)
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				// there shouldn't be an EOF here because
				// we actually got a length above. Return
				// ErrUnexpectedEOF instead of just EOF.
				// io.ReadFull already returns ErrUnexpectedEOF
				// if it failed to read blob as a whole but my
				// return io.EOF if it read exactly 0 bytes.
				readErr = io.ErrUnexpectedEOF
			}
			return readErr
		}

		// we don't really care about the format here but it must be
		// something that can encode/decode complex structures like
		// JSON, BSON or GenCode. So LoadAsFormat MUST return the value
		// passed as the third parameter. String or RAW encoding IS AN
		// error here.
		entry := &listEntry{}
		err := dsd.LoadAsFormat(blob, format, entry)
		if err != nil {
			return fmt.Errorf("failed to decoded DSD encoded entity: %w", err)
		}

		select {
		case ch <- entry:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func parseHeader(r io.Reader) (compressed bool, format byte, err error) {
	var listHeader [1]byte
	if _, err = r.Read(listHeader[:]); err != nil {
		// if we have an error here we can safely abort because
		// the file must be broken
		return compressed, format, err
	}

	if listHeader[0] != dsd.LIST {
		err = fmt.Errorf("unexpected file type: %d (%c), expected dsd list", listHeader[0], listHeader[0])

		return compressed, format, err
	}

	var compression [1]byte
	if _, err = r.Read(compression[:]); err != nil {
		// same here, a DSDL file must have at least 2 bytes header
		return compressed, format, err
	}

	if compression[0] == dsd.GZIP {
		compressed = true

		var formatSlice [1]byte
		if _, err = r.Read(formatSlice[:]); err != nil {
			return compressed, format, err
		}

		format = formatSlice[0]
		return compressed, format, err
	}

	format = compression[0]

	return compressed, format, err
}
