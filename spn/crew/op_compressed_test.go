package crew

import (
	"errors"
	"testing"
	"time"

	"github.com/safing/portmaster/spn/terminal"
	"github.com/safing/structures/container"
	"github.com/safing/structures/dsd"
)

// TestCrewOpsRejectCompressed checks that gzip compressed op data is rejected
// before decompression. dsd.Load decompresses it into an unbounded buffer, so
// a 16KB message could make the receiver allocate about 64MB.
func TestCrewOpsRejectCompressed(t *testing.T) {
	t.Parallel()

	nonce := []byte{1, 2, 3, 4}

	// Ping request.
	data, err := dsd.DumpAndCompress(&PingOpRequest{Nonce: nonce}, dsd.CBOR, dsd.GZIP)
	if err != nil {
		t.Fatal(err)
	}
	a, _, err := terminal.NewSimpleTestTerminalPair(0, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, tErr := startPingOp(a, 1, container.New(data))
	if !errors.Is(tErr, dsd.ErrIncompatibleFormat) {
		t.Errorf("ping request: expected ErrIncompatibleFormat, got %v", tErr)
	}

	// Ping response.
	data, err = dsd.DumpAndCompress(&PingOpResponse{Nonce: nonce, Time: time.Now()}, dsd.CBOR, dsd.GZIP)
	if err != nil {
		t.Fatal(err)
	}
	op := &PingOp{nonce: nonce}
	tErr = op.Deliver(terminal.NewMsg(data))
	if !errors.Is(tErr, dsd.ErrIncompatibleFormat) {
		t.Errorf("ping response: expected ErrIncompatibleFormat, got %v", tErr)
	}

	// Connect request.
	data, err = dsd.DumpAndCompress(&ConnectRequest{}, dsd.CBOR, dsd.GZIP)
	if err != nil {
		t.Fatal(err)
	}
	_, tErr = startConnectOp(a, 1, container.New(data))
	if !errors.Is(tErr, dsd.ErrIncompatibleFormat) {
		t.Errorf("connect request: expected ErrIncompatibleFormat, got %v", tErr)
	}
}
