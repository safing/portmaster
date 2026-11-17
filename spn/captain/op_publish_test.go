package captain

import (
	"bytes"
	"testing"

	"github.com/safing/structures/container"
	"github.com/safing/structures/varint"
)

// TestParsePublishOpDataWithHugeBlockSize checks that publish op init data
// with a crafted block size is rejected without allocating that size.
func TestParsePublishOpDataWithHugeBlockSize(t *testing.T) {
	t.Parallel()

	for _, size := range []uint64{1 << 42, 1 << 62, 1<<64 - 1, 4} {
		_, _, tErr := parsePublishOpData(container.New(varint.Pack64(size), []byte("abc")))
		if tErr == nil {
			t.Fatalf("block size %d accepted", size)
		}
	}
	// Status block with a huge size.
	c := container.New()
	c.AppendAsBlock([]byte("announcement"))
	c.Append(varint.Pack64(1 << 42))
	_, _, tErr := parsePublishOpData(c)
	if tErr == nil {
		t.Fatal("huge status block size accepted")
	}

	// Valid data must still parse.
	c = container.New()
	c.AppendAsBlock([]byte("announcement"))
	c.AppendAsBlock([]byte("status"))
	announcementData, statusData, tErr := parsePublishOpData(c)
	if tErr != nil {
		t.Fatal(tErr)
	}
	if !bytes.Equal(announcementData, []byte("announcement")) || !bytes.Equal(statusData, []byte("status")) {
		t.Fatalf("unexpected parse result: %q %q", announcementData, statusData)
	}
}
