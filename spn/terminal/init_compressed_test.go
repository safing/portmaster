package terminal

import (
	"errors"
	"testing"

	"github.com/safing/structures/container"
	"github.com/safing/structures/dsd"
	"github.com/safing/structures/varint"
)

// TestParseTerminalOptsRejectsCompressed checks that gzip compressed terminal
// options are rejected before decompression. They are received from any
// client before authentication, and dsd.Load decompresses them into an
// unbounded buffer, so 16KB could make a Hub allocate about 64MB.
func TestParseTerminalOptsRejectsCompressed(t *testing.T) {
	t.Parallel()

	opts := &TerminalOpts{Version: 1, Encrypt: true, Padding: 8, FlowControl: FlowControlDFQ, FlowControlSize: 100}

	// Packed options are accepted, as well as all serialization formats.
	packed, tErr := opts.Pack()
	if tErr != nil {
		t.Fatal(tErr)
	}
	parsed, tErr := ParseTerminalOpts(packed)
	if tErr != nil {
		t.Fatal(tErr)
	}
	if *parsed != *opts {
		t.Fatalf("unexpected options: %+v", parsed)
	}
	for _, format := range []uint8{dsd.JSON, dsd.CBOR, dsd.MsgPack, dsd.YAML} {
		data, err := dsd.Dump(opts, format)
		if err != nil {
			t.Fatal(err)
		}
		parsed, tErr := ParseTerminalOpts(container.New(varint.Pack8(1), data))
		if tErr != nil {
			t.Fatalf("format %c: %s", format, tErr)
		}
		if *parsed != *opts {
			t.Fatalf("format %c: unexpected options: %+v", format, parsed)
		}
	}

	// Compressed options are rejected.
	data, err := dsd.DumpAndCompress(opts, dsd.CBOR, dsd.GZIP)
	if err != nil {
		t.Fatal(err)
	}
	_, tErr = ParseTerminalOpts(container.New(varint.Pack8(1), data))
	if !errors.Is(tErr, dsd.ErrIncompatibleFormat) {
		t.Errorf("expected ErrIncompatibleFormat, got %v", tErr)
	}
}

// TestCounterOpRejectsCompressed checks that gzip compressed counter op
// options are rejected before decompression.
func TestCounterOpRejectsCompressed(t *testing.T) {
	t.Parallel()

	data, err := dsd.DumpAndCompress(&CounterOpts{}, dsd.JSON, dsd.GZIP)
	if err != nil {
		t.Fatal(err)
	}
	_, tErr := startCounterOp(nil, 1, container.New(data))
	if !errors.Is(tErr, dsd.ErrIncompatibleFormat) {
		t.Errorf("expected ErrIncompatibleFormat, got %v", tErr)
	}
}

func FuzzParseTerminalOpts(f *testing.F) {
	opts := &TerminalOpts{Version: 1, Encrypt: true, Padding: 8, FlowControl: FlowControlDFQ, FlowControlSize: 100}
	for _, format := range []uint8{dsd.JSON, dsd.CBOR, dsd.MsgPack, dsd.YAML} {
		data, err := dsd.Dump(opts, format)
		if err != nil {
			f.Fatal(err)
		}
		f.Add(append(varint.Pack8(1), data...))
	}
	f.Add([]byte{1, 'Y', '[', '0', ']'})

	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = ParseTerminalOpts(container.New(data))
	})
}
