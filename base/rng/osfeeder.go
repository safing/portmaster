package rng

import (
	"crypto/rand"
	"fmt"

	"github.com/safing/portmaster/service/mgr"
)

func osFeeder(ctx *mgr.WorkerCtx) error {
	entropyBytes := minFeedEntropy / 8
	feeder := NewFeeder()
	defer feeder.CloseFeeder()

	for {
		// gather
		osEntropy := make([]byte, entropyBytes)
		n, err := rand.Read(osEntropy)
		if err != nil {
			return fmt.Errorf("could not read entropy from os: %w", err)
		}
		if n != entropyBytes {
			return fmt.Errorf("could not read enough entropy from os: got only %d bytes instead of %d", n, entropyBytes)
		}

		// feed
		select {
		case feeder.input <- &entropyData{
			data:    osEntropy,
			entropy: entropyBytes * 8,
		}:
		case <-ctx.Done():
			return nil
		}
	}
}
