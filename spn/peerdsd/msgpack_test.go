package peerdsd

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/safing/structures/dsd"
)

type msgpackStruct struct {
	Data  []byte
	Items []*testStruct
	Names []string
	Map   map[string]int
	Time  time.Time
	F32   float32
	F64   float64
	I     int64
	U     uint64
	B     bool
	P     *testStruct
}

func validMsgPackStructs() []*msgpackStruct {
	items := make([]*testStruct, 20)
	for i := range items {
		items[i] = &testStruct{A: "item", B: i}
	}
	m := make(map[string]int, 20)
	for i := range 20 {
		m[string(rune('a'+i))] = i << (i * 3)
	}
	return []*msgpackStruct{
		{},
		{
			Data:  []byte("data"),
			Items: items[:3],
			Names: []string{"a", "bb", ""},
			Map:   map[string]int{"a": 1, "b": -1000000},
			Time:  time.Unix(1700000000, 123).UTC(),
			F32:   1.5,
			F64:   -2.5,
			I:     -1 << 40,
			U:     1 << 63,
			B:     true,
			P:     &testStruct{A: "p", B: 70000},
		},
		{
			Data:  bytes.Repeat([]byte{1}, 300),
			Items: items,
			Names: []string{string(bytes.Repeat([]byte{'s'}, 40)), string(bytes.Repeat([]byte{'s'}, 300))},
			Map:   m,
		},
		{Data: bytes.Repeat([]byte{1}, 70000)},
	}
}

// TestMsgPackLengthsAcceptsValid checks that msgpack data as produced by
// dsd.Dump is accepted and loaded.
func TestMsgPackLengthsAcceptsValid(t *testing.T) {
	t.Parallel()

	for i, s := range validMsgPackStructs() {
		data, err := dsd.Dump(s, dsd.MsgPack)
		if err != nil {
			t.Fatal(err)
		}
		err = CheckFormat(data)
		if err != nil {
			t.Errorf("struct %d: CheckFormat failed: %s", i, err)
		}
		loaded := &msgpackStruct{}
		_, err = Load(data, loaded)
		if err != nil {
			t.Errorf("struct %d: Load failed: %s", i, err)
		}
		if !bytes.Equal(loaded.Data, s.Data) || len(loaded.Items) != len(s.Items) ||
			len(loaded.Map) != len(s.Map) || !loaded.Time.Equal(s.Time) || loaded.U != s.U {
			t.Errorf("struct %d: unexpected result: %+v", i, loaded)
		}
	}
}

// TestMsgPackLengthsOnlyRejectsInvalid checks that data rejected by the
// msgpack length check also fails to load with dsd.Load, using all truncations
// of valid data.
func TestMsgPackLengthsOnlyRejectsInvalid(t *testing.T) {
	t.Parallel()

	var rejected int
	for i, s := range validMsgPackStructs()[:3] {
		data, err := dsd.Dump(s, dsd.MsgPack)
		if err != nil {
			t.Fatal(err)
		}
		for n := 2; n <= len(data); n++ {
			err := CheckFormat(data[:n])
			switch {
			case err == nil:
			case errors.Is(err, ErrLengthExceedsData):
				rejected++
				_, loadErr := dsd.Load(data[:n], &msgpackStruct{})
				if loadErr == nil {
					t.Errorf("struct %d truncated to %d bytes: rejected, but dsd.Load accepts it", i, n)
				}
			default:
				t.Errorf("struct %d truncated to %d bytes: unexpected error %s", i, n, err)
			}
		}
	}
	if rejected == 0 {
		t.Error("no truncated data was rejected")
	}
}

// TestMsgPackLengthsRejectsOversized checks that msgpack data that declares
// more data than it holds is rejected before decoding. The msgpack decoder
// allocates the declared length up front: 12 bytes declaring a 4GB byte string
// allocate 4GB, and declaring 2^32 pointers in an array allocates 32GB, which
// crashes the process with "fatal error: out of memory".
// The lengths used here are smaller to keep the test cheap without the fix.
func TestMsgPackLengthsRejectsOversized(t *testing.T) {
	t.Parallel()

	for name, data := range map[string][]byte{
		"bin32":           {'M', 0x81, 0xa4, 'D', 'a', 't', 'a', 0xc6, 0x01, 0x00, 0x00, 0x00},
		"bin16":           {'M', 0x81, 0xa4, 'D', 'a', 't', 'a', 0xc5, 0x01, 0x00, 1, 2, 3},
		"bin8":            {'M', 0x81, 0xa4, 'D', 'a', 't', 'a', 0xc4, 0x05, 1, 2, 3},
		"str32 key":       {'M', 0x81, 0xdb, 0x01, 0x00, 0x00, 0x00, 'a'},
		"fixstr key":      {'M', 0x81, 0xa9, 'D', 'a', 't', 'a'},
		"array32":         {'M', 0x81, 0xa5, 'I', 't', 'e', 'm', 's', 0xdd, 0x00, 0x10, 0x00, 0x00},
		"array16":         {'M', 0x81, 0xa5, 'N', 'a', 'm', 'e', 's', 0xdc, 0xff, 0xff, 0xc0},
		"map32":           {'M', 0x81, 0xa3, 'M', 'a', 'p', 0xdf, 0x00, 0x10, 0x00, 0x00},
		"top-level map32": {'M', 0xdf, 0x00, 0x10, 0x00, 0x00},
		"fixmap":          {'M', 0x8f, 0xc0, 0xc0},
		"ext32":           {'M', 0x81, 0xa4, 'T', 'i', 'm', 'e', 0xc9, 0x00, 0x10, 0x00, 0x00, 0xff},
		"ext32 no type":   {'M', 0x91, 0xc9, 0x00, 0x10, 0x00, 0x00},
		"nested":          {'M', 0x81, 0xa5, 'I', 't', 'e', 'm', 's', 0x91, 0x81, 0xa1, 'a', 0xdb, 0x00, 0x10, 0x00, 0x00},
	} {
		err := CheckFormat(data)
		if !errors.Is(err, ErrLengthExceedsData) {
			t.Errorf("%s: CheckFormat: expected ErrLengthExceedsData, got %v", name, err)
		}
		_, err = Load(data, &msgpackStruct{})
		if !errors.Is(err, ErrLengthExceedsData) {
			t.Errorf("%s: Load: expected ErrLengthExceedsData, got %v", name, err)
		}
	}
}

func FuzzLoad(f *testing.F) {
	for _, s := range validMsgPackStructs()[:3] {
		for _, format := range []uint8{dsd.JSON, dsd.CBOR, dsd.MsgPack, dsd.YAML} {
			data, err := dsd.Dump(s, format)
			if err != nil {
				f.Fatal(err)
			}
			f.Add(data)
		}
	}
	f.Add([]byte{'M', 0x81, 0xa4, 'D', 'a', 't', 'a', 0xc6, 0xff, 0xff, 0xff, 0xff})
	f.Add([]byte{'M', 0x81, 0xa5, 'I', 't', 'e', 'm', 's', 0xdd, 0xff, 0xff, 0xff, 0xff})

	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = Load(data, &msgpackStruct{})
	})
}
