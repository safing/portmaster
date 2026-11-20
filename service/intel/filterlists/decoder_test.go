package filterlists

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"encoding/json"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/safing/structures/dsd"
)

const testTypeDomain = "Domain"

func encodeTestList(t *testing.T, compressed bool, entries []listEntry) []byte {
	t.Helper()

	var body bytes.Buffer
	for _, e := range entries {
		data, err := json.Marshal(e)
		require.NoError(t, err)
		body.Write(binary.AppendUvarint(nil, uint64(len(data))))
		body.Write(data)
	}

	out := bytes.Buffer{}
	out.WriteByte(dsd.LIST)
	if !compressed {
		out.WriteByte(dsd.JSON)
		out.Write(body.Bytes())
		return out.Bytes()
	}
	out.WriteByte(dsd.GZIP)
	out.WriteByte(dsd.JSON)
	zw := gzip.NewWriter(&out)
	_, err := zw.Write(body.Bytes())
	require.NoError(t, err)
	require.NoError(t, zw.Close())
	return out.Bytes()
}

func TestDecodeFile(t *testing.T) {
	t.Parallel()

	// Entries of different lengths, so the reused buffer shrinks and grows.
	entries := []listEntry{
		{Type: testTypeDomain, Entity: "a-rather-long-domain-name.example.com", Resources: []entryResource{{SourceID: "TRAC", ResourceID: "x"}}},
		{Type: testTypeDomain, Entity: "b.org"},
		{Type: "IPv4", Entity: "192.0.2.1", Whitelist: true},
		{Type: testTypeDomain, Entity: "another-long-domain-name-for-growing-the-buffer.example.net"},
	}

	for _, compressed := range []bool{false, true} {
		ch := make(chan *listEntry, len(entries)+1)
		err := decodeFile(t.Context(), bytes.NewReader(encodeTestList(t, compressed, entries)), ch)
		require.NoError(t, err)
		close(ch)

		var got []listEntry
		for e := range ch {
			got = append(got, *e)
		}
		assert.Equal(t, entries, got, "compressed=%v", compressed)
	}
}

func BenchmarkDecodeFile(b *testing.B) {
	t := &testing.T{}
	entries := make([]listEntry, 20000)
	for i := range entries {
		entries[i] = listEntry{Type: testTypeDomain, Entity: "host-" + strconv.Itoa(i) + ".example.com", Resources: []entryResource{{SourceID: "TRAC", ResourceID: "x"}}}
	}
	data := encodeTestList(t, true, entries)

	b.ReportAllocs()
	for b.Loop() {
		ch := make(chan *listEntry, 128)
		go func() {
			for range ch { //nolint:revive // Drain.
			}
		}()
		err := decodeFile(b.Context(), bytes.NewReader(data), ch)
		if err != nil {
			b.Fatal(err)
		}
		close(ch)
	}
}
