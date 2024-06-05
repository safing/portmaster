//nolint:gocognit
package varint

import (
	"bytes"
	"testing"
)

func TestConversion(t *testing.T) {
	t.Parallel()

	subjects := []struct {
		intType uint8
		bytes   []byte
		integer uint64
	}{
		{8, []byte{0x00}, 0},
		{8, []byte{0x01}, 1},
		{8, []byte{0x7F}, 127},
		{8, []byte{0x80, 0x01}, 128},
		{8, []byte{0xFF, 0x01}, 255},

		{16, []byte{0x80, 0x02}, 256},
		{16, []byte{0xFF, 0x7F}, 16383},
		{16, []byte{0x80, 0x80, 0x01}, 16384},
		{16, []byte{0xFF, 0xFF, 0x03}, 65535},

		{32, []byte{0x80, 0x80, 0x04}, 65536},
		{32, []byte{0xFF, 0xFF, 0x7F}, 2097151},
		{32, []byte{0x80, 0x80, 0x80, 0x01}, 2097152},
		{32, []byte{0xFF, 0xFF, 0xFF, 0x07}, 16777215},
		{32, []byte{0x80, 0x80, 0x80, 0x08}, 16777216},
		{32, []byte{0xFF, 0xFF, 0xFF, 0x7F}, 268435455},
		{32, []byte{0x80, 0x80, 0x80, 0x80, 0x01}, 268435456},
		{32, []byte{0xFF, 0xFF, 0xFF, 0xFF, 0x0F}, 4294967295},

		{64, []byte{0x80, 0x80, 0x80, 0x80, 0x10}, 4294967296},
		{64, []byte{0xFF, 0xFF, 0xFF, 0xFF, 0x7F}, 34359738367},
		{64, []byte{0x80, 0x80, 0x80, 0x80, 0x80, 0x01}, 34359738368},
		{64, []byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0x1F}, 1099511627775},
		{64, []byte{0x80, 0x80, 0x80, 0x80, 0x80, 0x20}, 1099511627776},
		{64, []byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0x7F}, 4398046511103},
		{64, []byte{0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x01}, 4398046511104},
		{64, []byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0x3F}, 281474976710655},
		{64, []byte{0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x40}, 281474976710656},
		{64, []byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0x7F}, 562949953421311},
		{64, []byte{0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x01}, 562949953421312},
		{64, []byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0x7F}, 72057594037927935},

		{64, []byte{0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x01}, 72057594037927936},
		{64, []byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0x7F}, 9223372036854775807},

		{64, []byte{0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x01}, 9223372036854775808},
		{64, []byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0x01}, 18446744073709551615},
	}

	for _, subject := range subjects {

		actualInteger, _, err := Unpack64(subject.bytes)
		if err != nil || actualInteger != subject.integer {
			t.Errorf("Unpack64 %d: expected %d, actual %d", subject.bytes, subject.integer, actualInteger)
		}
		actualBytes := Pack64(subject.integer)
		if err != nil || !bytes.Equal(actualBytes, subject.bytes) {
			t.Errorf("Pack64 %d: expected %d, actual %d", subject.integer, subject.bytes, actualBytes)
		}

		if subject.intType <= 32 {
			actualInteger, _, err := Unpack32(subject.bytes)
			if err != nil || actualInteger != uint32(subject.integer) {
				t.Errorf("Unpack32 %d: expected %d, actual %d", subject.bytes, subject.integer, actualInteger)
			}
			actualBytes := Pack32(uint32(subject.integer))
			if err != nil || !bytes.Equal(actualBytes, subject.bytes) {
				t.Errorf("Pack32 %d: expected %d, actual %d", subject.integer, subject.bytes, actualBytes)
			}
		}

		if subject.intType <= 16 {
			actualInteger, _, err := Unpack16(subject.bytes)
			if err != nil || actualInteger != uint16(subject.integer) {
				t.Errorf("Unpack16 %d: expected %d, actual %d", subject.bytes, subject.integer, actualInteger)
			}
			actualBytes := Pack16(uint16(subject.integer))
			if err != nil || !bytes.Equal(actualBytes, subject.bytes) {
				t.Errorf("Pack16 %d: expected %d, actual %d", subject.integer, subject.bytes, actualBytes)
			}
		}

		if subject.intType <= 8 {
			actualInteger, _, err := Unpack8(subject.bytes)
			if err != nil || actualInteger != uint8(subject.integer) {
				t.Errorf("Unpack8 %d: expected %d, actual %d", subject.bytes, subject.integer, actualInteger)
			}
			actualBytes := Pack8(uint8(subject.integer))
			if err != nil || !bytes.Equal(actualBytes, subject.bytes) {
				t.Errorf("Pack8 %d: expected %d, actual %d", subject.integer, subject.bytes, actualBytes)
			}
		}

	}
}

func TestFails(t *testing.T) {
	t.Parallel()

	subjects := []struct {
		intType uint8
		bytes   []byte
	}{
		{32, []byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0x01}},
		{64, []byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0x02}},
		{64, []byte{0xFF}},
	}

	for _, subject := range subjects {

		if subject.intType == 64 {
			_, _, err := Unpack64(subject.bytes)
			if err == nil {
				t.Errorf("Unpack64 %d: expected error while unpacking.", subject.bytes)
			}
		}

		_, _, err := Unpack32(subject.bytes)
		if err == nil {
			t.Errorf("Unpack32 %d: expected error while unpacking.", subject.bytes)
		}

		_, _, err = Unpack16(subject.bytes)
		if err == nil {
			t.Errorf("Unpack16 %d: expected error while unpacking.", subject.bytes)
		}

		_, _, err = Unpack8(subject.bytes)
		if err == nil {
			t.Errorf("Unpack8 %d: expected error while unpacking.", subject.bytes)
		}

	}
}
