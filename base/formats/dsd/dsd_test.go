//nolint:maligned,gocyclo,gocognit
package dsd

import (
	"math/big"
	"reflect"
	"testing"
)

// SimpleTestStruct is used for testing.
type SimpleTestStruct struct {
	S string
	B byte
}

type ComplexTestStruct struct {
	I    int
	I8   int8
	I16  int16
	I32  int32
	I64  int64
	UI   uint
	UI8  uint8
	UI16 uint16
	UI32 uint32
	UI64 uint64
	BI   *big.Int
	S    string
	Sp   *string
	Sa   []string
	Sap  *[]string
	B    byte
	Bp   *byte
	Ba   []byte
	Bap  *[]byte
	M    map[string]string
	Mp   *map[string]string
}

type GenCodeTestStruct struct {
	I8   int8
	I16  int16
	I32  int32
	I64  int64
	UI8  uint8
	UI16 uint16
	UI32 uint32
	UI64 uint64
	S    string
	Sp   *string
	Sa   []string
	Sap  *[]string
	B    byte
	Bp   *byte
	Ba   []byte
	Bap  *[]byte
}

var (
	simpleSubject = &SimpleTestStruct{
		"a",
		0x01,
	}

	bString      = "b"
	bBytes  byte = 0x02

	complexSubject = &ComplexTestStruct{
		-1,
		-2,
		-3,
		-4,
		-5,
		1,
		2,
		3,
		4,
		5,
		big.NewInt(6),
		"a",
		&bString,
		[]string{"c", "d", "e"},
		&[]string{"f", "g", "h"},
		0x01,
		&bBytes,
		[]byte{0x03, 0x04, 0x05},
		&[]byte{0x05, 0x06, 0x07},
		map[string]string{
			"a": "b",
			"c": "d",
			"e": "f",
		},
		&map[string]string{
			"g": "h",
			"i": "j",
			"k": "l",
		},
	}

	genCodeSubject = &GenCodeTestStruct{
		-2,
		-3,
		-4,
		-5,
		2,
		3,
		4,
		5,
		"a",
		&bString,
		[]string{"c", "d", "e"},
		&[]string{"f", "g", "h"},
		0x01,
		&bBytes,
		[]byte{0x03, 0x04, 0x05},
		&[]byte{0x05, 0x06, 0x07},
	}
)

func TestConversion(t *testing.T) { //nolint:maintidx
	t.Parallel()

	compressionFormats := []uint8{AUTO, GZIP}
	formats := []uint8{JSON, CBOR, MsgPack}

	for _, compression := range compressionFormats {
		for _, format := range formats {

			// simple
			var b []byte
			var err error
			if compression != AUTO {
				b, err = DumpAndCompress(simpleSubject, format, compression)
			} else {
				b, err = Dump(simpleSubject, format)
			}
			if err != nil {
				t.Fatalf("Dump error (simple struct): %s", err)
			}

			si := &SimpleTestStruct{}
			_, err = Load(b, si)
			if err != nil {
				t.Fatalf("Load error (simple struct): %s", err)
			}

			if !reflect.DeepEqual(simpleSubject, si) {
				t.Errorf("Load (simple struct): subject does not match loaded object")
				t.Errorf("Encoded: %v", string(b))
				t.Errorf("Compared: %v == %v", simpleSubject, si)
			}

			// complex
			if compression != AUTO {
				b, err = DumpAndCompress(complexSubject, format, compression)
			} else {
				b, err = Dump(complexSubject, format)
			}
			if err != nil {
				t.Fatalf("Dump error (complex struct): %s", err)
			}

			co := &ComplexTestStruct{}
			_, err = Load(b, co)
			if err != nil {
				t.Fatalf("Load error (complex struct): %s", err)
			}

			if complexSubject.I != co.I {
				t.Errorf("Load (complex struct): struct.I is not equal (%v != %v)", complexSubject.I, co.I)
			}
			if complexSubject.I8 != co.I8 {
				t.Errorf("Load (complex struct): struct.I8 is not equal (%v != %v)", complexSubject.I8, co.I8)
			}
			if complexSubject.I16 != co.I16 {
				t.Errorf("Load (complex struct): struct.I16 is not equal (%v != %v)", complexSubject.I16, co.I16)
			}
			if complexSubject.I32 != co.I32 {
				t.Errorf("Load (complex struct): struct.I32 is not equal (%v != %v)", complexSubject.I32, co.I32)
			}
			if complexSubject.I64 != co.I64 {
				t.Errorf("Load (complex struct): struct.I64 is not equal (%v != %v)", complexSubject.I64, co.I64)
			}
			if complexSubject.UI != co.UI {
				t.Errorf("Load (complex struct): struct.UI is not equal (%v != %v)", complexSubject.UI, co.UI)
			}
			if complexSubject.UI8 != co.UI8 {
				t.Errorf("Load (complex struct): struct.UI8 is not equal (%v != %v)", complexSubject.UI8, co.UI8)
			}
			if complexSubject.UI16 != co.UI16 {
				t.Errorf("Load (complex struct): struct.UI16 is not equal (%v != %v)", complexSubject.UI16, co.UI16)
			}
			if complexSubject.UI32 != co.UI32 {
				t.Errorf("Load (complex struct): struct.UI32 is not equal (%v != %v)", complexSubject.UI32, co.UI32)
			}
			if complexSubject.UI64 != co.UI64 {
				t.Errorf("Load (complex struct): struct.UI64 is not equal (%v != %v)", complexSubject.UI64, co.UI64)
			}
			if complexSubject.BI.Cmp(co.BI) != 0 {
				t.Errorf("Load (complex struct): struct.BI is not equal (%v != %v)", complexSubject.BI, co.BI)
			}
			if complexSubject.S != co.S {
				t.Errorf("Load (complex struct): struct.S is not equal (%v != %v)", complexSubject.S, co.S)
			}
			if !reflect.DeepEqual(complexSubject.Sp, co.Sp) {
				t.Errorf("Load (complex struct): struct.Sp is not equal (%v != %v)", complexSubject.Sp, co.Sp)
			}
			if !reflect.DeepEqual(complexSubject.Sa, co.Sa) {
				t.Errorf("Load (complex struct): struct.Sa is not equal (%v != %v)", complexSubject.Sa, co.Sa)
			}
			if !reflect.DeepEqual(complexSubject.Sap, co.Sap) {
				t.Errorf("Load (complex struct): struct.Sap is not equal (%v != %v)", complexSubject.Sap, co.Sap)
			}
			if complexSubject.B != co.B {
				t.Errorf("Load (complex struct): struct.B is not equal (%v != %v)", complexSubject.B, co.B)
			}
			if !reflect.DeepEqual(complexSubject.Bp, co.Bp) {
				t.Errorf("Load (complex struct): struct.Bp is not equal (%v != %v)", complexSubject.Bp, co.Bp)
			}
			if !reflect.DeepEqual(complexSubject.Ba, co.Ba) {
				t.Errorf("Load (complex struct): struct.Ba is not equal (%v != %v)", complexSubject.Ba, co.Ba)
			}
			if !reflect.DeepEqual(complexSubject.Bap, co.Bap) {
				t.Errorf("Load (complex struct): struct.Bap is not equal (%v != %v)", complexSubject.Bap, co.Bap)
			}
			if !reflect.DeepEqual(complexSubject.M, co.M) {
				t.Errorf("Load (complex struct): struct.M is not equal (%v != %v)", complexSubject.M, co.M)
			}
			if !reflect.DeepEqual(complexSubject.Mp, co.Mp) {
				t.Errorf("Load (complex struct): struct.Mp is not equal (%v != %v)", complexSubject.Mp, co.Mp)
			}

		}

		// test all formats
		simplifiedFormatTesting := []uint8{JSON, CBOR, MsgPack, GenCode}

		for _, format := range simplifiedFormatTesting {

			// simple
			var b []byte
			var err error
			if compression != AUTO {
				b, err = DumpAndCompress(simpleSubject, format, compression)
			} else {
				b, err = Dump(simpleSubject, format)
			}
			if err != nil {
				t.Fatalf("Dump error (simple struct): %s", err)
			}

			si := &SimpleTestStruct{}
			_, err = Load(b, si)
			if err != nil {
				t.Fatalf("Load error (simple struct): %s", err)
			}

			if !reflect.DeepEqual(simpleSubject, si) {
				t.Errorf("Load (simple struct): subject does not match loaded object")
				t.Errorf("Encoded: %v", string(b))
				t.Errorf("Compared: %v == %v", simpleSubject, si)
			}

			// complex
			b, err = DumpAndCompress(genCodeSubject, format, compression)
			if err != nil {
				t.Fatalf("Dump error (complex struct): %s", err)
			}

			co := &GenCodeTestStruct{}
			_, err = Load(b, co)
			if err != nil {
				t.Fatalf("Load error (complex struct): %s", err)
			}

			if genCodeSubject.I8 != co.I8 {
				t.Errorf("Load (complex struct): struct.I8 is not equal (%v != %v)", genCodeSubject.I8, co.I8)
			}
			if genCodeSubject.I16 != co.I16 {
				t.Errorf("Load (complex struct): struct.I16 is not equal (%v != %v)", genCodeSubject.I16, co.I16)
			}
			if genCodeSubject.I32 != co.I32 {
				t.Errorf("Load (complex struct): struct.I32 is not equal (%v != %v)", genCodeSubject.I32, co.I32)
			}
			if genCodeSubject.I64 != co.I64 {
				t.Errorf("Load (complex struct): struct.I64 is not equal (%v != %v)", genCodeSubject.I64, co.I64)
			}
			if genCodeSubject.UI8 != co.UI8 {
				t.Errorf("Load (complex struct): struct.UI8 is not equal (%v != %v)", genCodeSubject.UI8, co.UI8)
			}
			if genCodeSubject.UI16 != co.UI16 {
				t.Errorf("Load (complex struct): struct.UI16 is not equal (%v != %v)", genCodeSubject.UI16, co.UI16)
			}
			if genCodeSubject.UI32 != co.UI32 {
				t.Errorf("Load (complex struct): struct.UI32 is not equal (%v != %v)", genCodeSubject.UI32, co.UI32)
			}
			if genCodeSubject.UI64 != co.UI64 {
				t.Errorf("Load (complex struct): struct.UI64 is not equal (%v != %v)", genCodeSubject.UI64, co.UI64)
			}
			if genCodeSubject.S != co.S {
				t.Errorf("Load (complex struct): struct.S is not equal (%v != %v)", genCodeSubject.S, co.S)
			}
			if !reflect.DeepEqual(genCodeSubject.Sp, co.Sp) {
				t.Errorf("Load (complex struct): struct.Sp is not equal (%v != %v)", genCodeSubject.Sp, co.Sp)
			}
			if !reflect.DeepEqual(genCodeSubject.Sa, co.Sa) {
				t.Errorf("Load (complex struct): struct.Sa is not equal (%v != %v)", genCodeSubject.Sa, co.Sa)
			}
			if !reflect.DeepEqual(genCodeSubject.Sap, co.Sap) {
				t.Errorf("Load (complex struct): struct.Sap is not equal (%v != %v)", genCodeSubject.Sap, co.Sap)
			}
			if genCodeSubject.B != co.B {
				t.Errorf("Load (complex struct): struct.B is not equal (%v != %v)", genCodeSubject.B, co.B)
			}
			if !reflect.DeepEqual(genCodeSubject.Bp, co.Bp) {
				t.Errorf("Load (complex struct): struct.Bp is not equal (%v != %v)", genCodeSubject.Bp, co.Bp)
			}
			if !reflect.DeepEqual(genCodeSubject.Ba, co.Ba) {
				t.Errorf("Load (complex struct): struct.Ba is not equal (%v != %v)", genCodeSubject.Ba, co.Ba)
			}
			if !reflect.DeepEqual(genCodeSubject.Bap, co.Bap) {
				t.Errorf("Load (complex struct): struct.Bap is not equal (%v != %v)", genCodeSubject.Bap, co.Bap)
			}
		}

	}
}
