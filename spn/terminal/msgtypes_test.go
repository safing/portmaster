package terminal

import (
	"bytes"
	"testing"

	"github.com/safing/structures/container"
)

func TestMakeMsg(t *testing.T) {
	t.Parallel()

	for _, id := range []uint32{0, 4, 8, 124, 128, 1 << 14, 1<<28 + 4} {
		for _, msgType := range []MsgType{MsgTypeInit, MsgTypeData, MsgTypePriorityData, MsgTypeStop} {
			for _, size := range []int{0, 1, 124, 125, 126, 127, 128, 1400, 16380, 16384} {
				data := bytes.Repeat([]byte{0xAB}, size)

				// Build expected msg the original way.
				expected := container.New(data)
				AddIDType(expected, id, msgType)
				expected.PrependLength()

				// Build with MakeMsg and compare.
				c := container.New(data)
				MakeMsg(c, id, msgType)
				if !bytes.Equal(c.CompileData(), expected.CompileData()) {
					t.Fatalf("id=%d type=%d size=%d: MakeMsg output differs", id, msgType, size)
				}
			}
		}
	}
}
