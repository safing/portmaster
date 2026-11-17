package terminal

import (
	"bytes"
	"errors"
	"reflect"
	"testing"

	"github.com/safing/jess"
	"github.com/safing/structures/container"
	"github.com/safing/structures/varint"
)

func testWireLetters() []*jess.Letter {
	return []*jess.Letter{
		{ // Data message.
			Nonce: []byte{1, 2, 3, 4},
			Data:  []byte("encrypted data"),
			Mac:   []byte("mac"),
		},
		{ // Setup message with keys.
			Version: 1,
			SuiteID: jess.SuiteWireV1,
			Keys: []*jess.Seal{
				{ID: "key-id", Value: bytes.Repeat([]byte{7}, 32)},
				{Value: bytes.Repeat([]byte{8}, 32)},
			},
			Nonce: []byte{1, 2, 3, 4},
			Data:  bytes.Repeat([]byte("data"), 1000),
		},
		{ // Rekeying message.
			ApplyKeys: true,
			Keys:      []*jess.Seal{{Value: bytes.Repeat([]byte{9}, 32)}},
			Data:      []byte{},
		},
	}
}

// TestLetterFromWireMatchesJess checks that LetterFromWire parses valid
// letters exactly like jess.LetterFromWire.
func TestLetterFromWireMatchesJess(t *testing.T) {
	t.Parallel()

	for i, letter := range testWireLetters() {
		wire, err := letter.ToWire()
		if err != nil {
			t.Fatal(err)
		}
		data := wire.CompileData()

		expected, err := jess.LetterFromWire(container.New(data))
		if err != nil {
			t.Fatal(err)
		}
		// Also parse data that is split over multiple compartments.
		for _, c := range []*container.Container{
			container.New(data),
			container.New(data[:3], data[3:7], data[7:]),
		} {
			got, err := LetterFromWire(c)
			if err != nil {
				t.Fatalf("letter %d: %s", i, err)
			}
			if !reflect.DeepEqual(got, expected) {
				t.Errorf("letter %d: got %+v, expected %+v", i, got, expected)
			}
			if c.Length() != 0 {
				t.Errorf("letter %d: %d bytes left", i, c.Length())
			}
		}
	}
}

// TestLetterFromWireChecksBlockSizes checks that letters with a block size
// that exceeds the available data are rejected before the block is read.
// jess.LetterFromWire allocates the block size first, which crashes the
// process with "fatal error: runtime: out of memory" for these letters.
func TestLetterFromWireChecksBlockSizes(t *testing.T) {
	t.Parallel()

	hugeSizes := []uint64{1 << 42, 1 << 62, 1<<64 - 1}
	// Letter prefixes up to the block that gets the huge size.
	prefixes := map[string][]byte{
		"suite ID":  {1, 1, 1},                // Setup message: version, then suite ID.
		"signet ID": {1, 2, 1},                // One key: signet ID.
		"seal":      {1, 2, 1, 0},             // One key with empty signet ID: seal value.
		"nonce":     {1, 0},                   // Data message: nonce.
		"data":      {1, 0, 1, 0xFF},          // Data message with nonce: data.
		"mac":       {1, 0, 1, 0xFF, 1, 0xFF}, // Data message with nonce and data: mac.
	}

	for name, prefix := range prefixes {
		for _, size := range hugeSizes {
			data := append(bytes.Clone(prefix), varint.Pack64(size)...)
			data = append(data, "some data"...)
			_, err := LetterFromWire(container.New(data))
			if !errors.Is(err, ErrBlockTooBig) {
				t.Errorf("%s with size %d: expected ErrBlockTooBig, got %v", name, size, err)
			}
		}
	}

	// Empty containers and truncated letters fail without panic.
	for _, c := range []*container.Container{
		container.New(),
		container.New(nil),
		container.New([]byte{1}),
		container.New([]byte{1, 0}),
		container.New([]byte{1, 3, 1}),
		container.New([]byte{1, 2, 255}),
		container.New([]byte{2, 0}),
	} {
		_, err := LetterFromWire(c)
		if err == nil {
			t.Errorf("expected error for %v", c.CompileData())
		}
	}
}

// TestDecryptLetterWithHugeBlockSize checks that a terminal does not allocate
// the block size of a received letter, which is parsed before the letter is
// authenticated. Before LetterFromWire was used, this test crashed with
// "fatal error: runtime: out of memory".
func TestDecryptLetterWithHugeBlockSize(t *testing.T) {
	t.Parallel()

	term := &TerminalBase{opts: &TerminalOpts{Encrypt: true}}
	// Data message with a nonce of 4TB.
	data := append([]byte{1, 0}, varint.Pack64(1<<42)...)
	_, tErr := term.decrypt(container.New(data))
	if tErr == nil || !tErr.Is(ErrMalformedData) || !errors.Is(tErr, ErrBlockTooBig) {
		t.Errorf("expected malformed data error with ErrBlockTooBig, got %v", tErr)
	}
}

// FuzzLetterFromWire checks that LetterFromWire never panics and agrees with
// jess.LetterFromWire on everything it accepts.
func FuzzLetterFromWire(f *testing.F) {
	for _, letter := range testWireLetters() {
		wire, err := letter.ToWire()
		if err != nil {
			f.Fatal(err)
		}
		f.Add(wire.CompileData())
	}
	f.Add([]byte{1, 0, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0x0F})
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, data []byte) {
		got, err := LetterFromWire(container.New(bytes.Clone(data)))
		if err != nil {
			// jess.LetterFromWire cannot be called here, as it may run out of memory.
			return
		}
		expected, err := jess.LetterFromWire(container.New(bytes.Clone(data)))
		if err != nil {
			t.Fatalf("jess.LetterFromWire failed on accepted letter: %s", err)
		}
		if !reflect.DeepEqual(got, expected) {
			t.Fatalf("got %+v, expected %+v", got, expected)
		}
	})
}
