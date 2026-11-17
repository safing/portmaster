package docks

import (
	"bytes"
	"testing"
	"time"

	"github.com/safing/portmaster/spn/ships"
	"github.com/safing/portmaster/spn/terminal"
	"github.com/safing/structures/container"
	"github.com/safing/structures/varint"
)

// TestCraneLoaderFraming checks that the loader produces the same byte stream
// as packing every message with terminal.MakeMsg and splitting the result
// into shipments of at most the target load size.
func TestCraneLoaderFraming(t *testing.T) { //nolint:paralleltest // Modifies the global optimalMinLoadSize.
	optimalMinLoadSize = 3072
	ship := ships.NewTestShip(true, ships.BaseMTU)
	crane, err := NewCrane(ship, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer unregisterCrane(crane)
	reverse := ship.Reverse()

	// Send messages of different sizes, so that shipments are split.
	sizes := []int{1, 100, 1400, 127, 128, 5000, 16000, 3, 1400, 1400, 1400, 300}
	expected := container.New()
	var expectedLen int
	for i, size := range sizes {
		data := bytes.Repeat([]byte{byte(i + 1)}, size)

		msg := terminal.NewMsg(data)
		msg.FlowID = uint32(8 + 4*i)
		msg.Type = terminal.MsgTypeData
		crane.terminalMsgs <- msg

		c := container.New(data)
		terminal.MakeMsg(c, uint32(8+4*i), terminal.MsgTypeData)
		expectedLen += c.Length()
		expected.AppendContainer(c)
	}
	expectedData := expected.CompileData()

	// Start loader.
	loaderDone := make(chan struct{})
	go func() {
		defer close(loaderDone)
		_ = crane.loader(nil)
	}()

	// Receive shipments until all data is received.
	var received []byte
	buf := make([]byte, 100000)
	for len(received) < expectedLen {
		n, err := reverse.UnloadTo(buf)
		if err != nil {
			t.Fatal(err)
		}
		shipmentLen, lenSize, err := varint.Unpack64(buf[:n])
		if err != nil {
			t.Fatal(err)
		}
		if shipmentLen != uint64(n-lenSize) { //nolint:gosec // Not negative.
			t.Fatalf("shipment length %d does not match received data %d", shipmentLen, n-lenSize)
		}
		if shipmentLen > uint64(crane.targetLoadSize) { //nolint:gosec // Not negative.
			t.Fatalf("shipment length %d exceeds target load size %d", shipmentLen, crane.targetLoadSize)
		}
		received = append(received, buf[lenSize:n]...)
	}

	if !bytes.Equal(received, expectedData) {
		t.Fatal("loaded data does not match expected framing")
	}

	crane.cancelCtx()
	select {
	case <-loaderDone:
	case <-time.After(5 * time.Second):
		t.Fatal("loader did not stop")
	}
}

// TestCraneLoadPadding checks the padding and length prefix of loaded shipments.
func TestCraneLoadPadding(t *testing.T) { //nolint:paralleltest // Modifies the global optimalMinLoadSize.
	optimalMinLoadSize = 3072
	ship := ships.NewTestShip(true, ships.BaseMTU)
	crane, err := NewCrane(ship, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer unregisterCrane(crane)
	crane.opts.Padding = 64
	reverse := ship.Reverse()

	buf := make([]byte, 100000)
	for _, size := range []int{1, 63, 64, 100, 126, 127, 1000, crane.targetLoadSize - 10, crane.targetLoadSize} {
		data := bytes.Repeat([]byte{0xAA}, size)
		err := crane.load(data)
		if err != nil {
			t.Fatal(err)
		}
		n, err := reverse.UnloadTo(buf)
		if err != nil {
			t.Fatal(err)
		}

		// Calculate expected padding.
		paddingNeeded := 64 - ((size + varint.EncodedSize(uint64(size))) % 64) //nolint:gosec // Not negative.
		paddingNeeded = min(paddingNeeded, crane.targetLoadSize-size)

		shipmentLen, lenSize, err := varint.Unpack64(buf[:n])
		if err != nil {
			t.Fatal(err)
		}
		if shipmentLen != uint64(size+paddingNeeded) || shipmentLen != uint64(n-lenSize) { //nolint:gosec // Not negative.
			t.Fatalf("size %d: unexpected shipment length %d (received %d), expected %d", size, shipmentLen, n-lenSize, size+paddingNeeded)
		}
		shipment := buf[lenSize:n]
		if !bytes.Equal(shipment[:size], data) {
			t.Fatalf("size %d: data mismatch", size)
		}
		if paddingNeeded > 0 && shipment[size] != 0 {
			t.Fatalf("size %d: missing padding indicator", size)
		}
	}
}
