package terminal

import (
	"bytes"
	"errors"
	"testing"

	"github.com/safing/structures/container"
	"github.com/safing/structures/varint"
)

// TestGetNextBlockChecksSize checks that a block with a crafted size is
// rejected without allocating that size. container.GetNextBlock crashes the
// process with "fatal error: runtime: out of memory" on the large sizes.
func TestGetNextBlockChecksSize(t *testing.T) {
	t.Parallel()

	for _, size := range []uint64{1 << 42, 1 << 62, 1<<64 - 1, 4} {
		_, err := GetNextBlock(container.New(varint.Pack64(size), []byte("abc")))
		if !errors.Is(err, ErrBlockTooBig) {
			t.Errorf("block size %d: expected ErrBlockTooBig, got %v", size, err)
		}
	}

	// Empty and exhausted containers must fail without panicking.
	_, err := GetNextBlock(container.New())
	if err == nil {
		t.Error("empty container: expected error")
	}
	c := container.New([]byte{1, 'a'})
	_, err = GetNextBlock(c)
	if err != nil {
		t.Fatal(err)
	}
	_, err = GetNextBlock(c)
	if err == nil {
		t.Error("exhausted container: expected error")
	}

	// Valid blocks parse as before, also across compartments.
	c = container.New()
	c.AppendAsBlock([]byte("first"))
	c.Append(varint.Pack64(6))
	c.Append([]byte("sec"))
	c.Append([]byte("ond"))
	c.AppendAsBlock(nil)
	for _, expected := range []string{"first", "second", ""} {
		data, err := GetNextBlock(c)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(data, []byte(expected)) {
			t.Errorf("expected block %q, got %q", expected, data)
		}
	}
}

// TestOperationStartWithHugeOpTypeSize checks that an operation start with a
// crafted op type block size stops the operation instead of crashing.
func TestOperationStartWithHugeOpTypeSize(t *testing.T) {
	t.Parallel()

	a, _, err := NewSimpleTestTerminalPair(0, 10, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Abandon(nil)

	// Before the size check, this crashed the whole process.
	a.handleOperationStart(8, container.New(varint.Pack64(1<<42), []byte("abc")))
	if _, ok := a.GetActiveOp(8); ok {
		t.Fatal("operation with malformed init data was started")
	}
}
