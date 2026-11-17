package peerdsd

import (
	"bytes"
	"compress/gzip"
	"errors"
	"testing"

	"github.com/safing/structures/dsd"
)

type testStruct struct {
	A string `json:"a"`
	B int    `json:"b"`
}

// gzipDSD returns the given dsd data compressed with gzip, as produced by
// dsd.DumpAndCompress, with padding spaces inserted after the format.
// The padding is valid JSON whitespace, so the compressed data is a valid
// message that dsd.Load accepts.
func gzipDSD(tb testing.TB, data []byte, padding int) []byte {
	tb.Helper()

	buf := bytes.NewBuffer([]byte{dsd.GZIP})
	w, err := gzip.NewWriterLevel(buf, gzip.BestCompression)
	if err != nil {
		tb.Fatal(err)
	}
	_, _ = w.Write(data[:1])
	_, _ = w.Write(bytes.Repeat([]byte{' '}, padding))
	_, _ = w.Write(data[1:])
	err = w.Close()
	if err != nil {
		tb.Fatal(err)
	}
	return buf.Bytes()
}

func TestLoadRejectsCompressed(t *testing.T) {
	t.Parallel()

	data, err := dsd.DumpAndCompress(&testStruct{A: "a", B: 1}, dsd.JSON, dsd.GZIP)
	if err != nil {
		t.Fatal(err)
	}
	bomb := gzipDSD(t, []byte(`J{"a":"a","b":1}`), 1<<20)

	for _, d := range [][]byte{data, bomb} {
		// dsd.Load decompresses it.
		s := &testStruct{}
		_, err = dsd.Load(d, s)
		if err != nil || s.A != "a" {
			t.Fatalf("dsd.Load: unexpected result %+v, %v", s, err)
		}

		// Load and CheckFormat reject it.
		s = &testStruct{}
		_, err = Load(d, s)
		if !errors.Is(err, dsd.ErrIncompatibleFormat) {
			t.Errorf("Load: expected ErrIncompatibleFormat, got %v", err)
		}
		if *s != (testStruct{}) {
			t.Errorf("Load: struct was modified: %+v", s)
		}
		err = CheckFormat(d)
		if !errors.Is(err, dsd.ErrIncompatibleFormat) {
			t.Errorf("CheckFormat: expected ErrIncompatibleFormat, got %v", err)
		}
	}
}

func TestLoadAcceptsSerializationFormats(t *testing.T) {
	t.Parallel()

	for _, format := range []uint8{dsd.JSON, dsd.CBOR, dsd.MsgPack, dsd.YAML} {
		data, err := dsd.Dump(&testStruct{A: "a", B: 1}, format)
		if err != nil {
			t.Fatal(err)
		}
		err = CheckFormat(data)
		if err != nil {
			t.Errorf("format %c: CheckFormat failed: %s", format, err)
		}
		s := &testStruct{}
		loadedFormat, err := Load(data, s)
		if err != nil {
			t.Errorf("format %c: Load failed: %s", format, err)
		}
		if loadedFormat != format || *s != (testStruct{A: "a", B: 1}) {
			t.Errorf("format %c: unexpected result %c %+v", format, loadedFormat, s)
		}
	}
}

// TestLoadSameAsDSD checks that Load behaves exactly like dsd.Load for every
// format other than compression.
func TestLoadSameAsDSD(t *testing.T) {
	t.Parallel()

	payloads := [][]byte{
		nil,
		{},
		[]byte(`{"a":"a","b":1}`),
		{0xa1, 0x61, 0x61, 0x61, 0x61}, // CBOR/MsgPack-ish.
		[]byte("a: a\nb: 1\n"),
		{0x80},
	}
	for format := range 256 {
		if format == dsd.GZIP {
			continue
		}
		for _, payload := range payloads {
			data := append([]byte{byte(format)}, payload...)
			s1, s2 := &testStruct{}, &testStruct{}
			f1, err1 := dsd.Load(data, s1)
			f2, err2 := Load(data, s2)
			if f1 != f2 || *s1 != *s2 || (err1 == nil) != (err2 == nil) ||
				(err1 != nil && err1.Error() != err2.Error()) {
				t.Errorf("format %d, payload %q: dsd.Load = (%d, %+v, %v), Load = (%d, %+v, %v)",
					format, payload, f1, s1, err1, f2, s2, err2)
			}
		}
	}
	// Multi-byte varint format.
	data := []byte{0x80, 0x01, '{', '}'}
	_, err1 := dsd.Load(data, &testStruct{})
	_, err2 := Load(data, &testStruct{})
	if !errors.Is(err1, dsd.ErrIncompatibleFormat) || !errors.Is(err2, dsd.ErrIncompatibleFormat) {
		t.Errorf("multi-byte format: unexpected errors %v, %v", err1, err2)
	}
}

// BenchmarkLoadGzipBomb measures the cost of a 16KB gzip compressed message,
// the maximum size of an SPN segment (spn/docks/crane.go maxSegmentLength).
func BenchmarkLoadGzipBomb(b *testing.B) {
	// Find padding that results in a message just below 16KB.
	padding := 16 << 20
	bomb := gzipDSD(b, []byte(`J{"a":"a","b":1}`), padding)
	for len(bomb) > 16384 {
		padding -= 64 << 10
		bomb = gzipDSD(b, []byte(`J{"a":"a","b":1}`), padding)
	}
	b.Logf("message size: %d bytes, decompressed: %d bytes", len(bomb), padding+16)

	b.Run("dsd.Load", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			_, _ = dsd.Load(bomb, &testStruct{})
		}
	})
	b.Run("peerdsd.Load", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			_, _ = Load(bomb, &testStruct{})
		}
	})
}
