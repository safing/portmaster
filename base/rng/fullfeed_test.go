package rng

import (
	"testing"
)

func TestFullFeeder(t *testing.T) {
	t.Parallel()

	for range 10 {
		go func() {
			rngFeeder <- []byte{0}
		}()
	}
}
