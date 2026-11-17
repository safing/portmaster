package docks

import (
	"errors"
	"testing"

	"github.com/safing/portmaster/spn/terminal"
	"github.com/safing/structures/container"
	"github.com/safing/structures/dsd"
)

// TestWhoAmIRejectsCompressed checks that a gzip compressed whoami response
// is rejected before decompression. dsd.Load decompresses it into an
// unbounded buffer, so a 16KB message could make the receiver allocate about
// 64MB.
func TestWhoAmIRejectsCompressed(t *testing.T) {
	t.Parallel()

	data, err := dsd.DumpAndCompress(&WhoAmIResponse{Addr: "1.2.3.4:5"}, dsd.CBOR, dsd.GZIP)
	if err != nil {
		t.Fatal(err)
	}
	op := &WhoAmIOp{}
	tErr := op.Deliver(terminal.NewMsg(data))
	if !errors.Is(tErr, dsd.ErrIncompatibleFormat) {
		t.Errorf("expected ErrIncompatibleFormat, got %v", tErr)
	}
}

// TestCapacityOpRejectsCompressed checks that gzip compressed capacity test
// options are rejected before decompression.
func TestCapacityOpRejectsCompressed(t *testing.T) { //nolint:paralleltest // Uses the global capacity test lock.
	data, err := dsd.DumpAndCompress(&CapacityTestOptions{TestVolume: maxCapacityTestVolume + 1}, dsd.CBOR, dsd.GZIP)
	if err != nil {
		t.Fatal(err)
	}
	_, tErr := startCapacityTestOp(nil, 1, container.New(data))
	if !errors.Is(tErr, dsd.ErrIncompatibleFormat) {
		t.Errorf("expected ErrIncompatibleFormat, got %v", tErr)
	}
}
