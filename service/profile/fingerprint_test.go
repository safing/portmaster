package profile

import (
	"math/rand"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestDeriveProfileID(t *testing.T) {
	t.Parallel()

	fps := []Fingerprint{
		{
			Type:      FingerprintTypePathID,
			Operation: FingerprintOperationEqualsID,
			Value:     "/sbin/init",
		},
		{
			Type:      FingerprintTypePathID,
			Operation: FingerprintOperationPrefixID,
			Value:     "/",
		},
		{
			Type:      FingerprintTypeEnvID,
			Key:       "PORTMASTER_PROFILE",
			Operation: FingerprintOperationEqualsID,
			Value:     "TEST-1",
		},
		{
			Type:      FingerprintTypeTagID,
			Key:       "tag-key-1",
			Operation: FingerprintOperationEqualsID,
			Value:     "tag-key-2",
		},
	}

	// Create rand source for shuffling.
	rnd := rand.New(rand.NewSource(time.Now().UnixNano())) //nolint:gosec

	// Test 100 times.
	for range 100 {
		// Shuffle fingerprints.
		rnd.Shuffle(len(fps), func(i, j int) {
			fps[i], fps[j] = fps[j], fps[i]
		})

		// Check if fingerprint matches.
		id := DeriveProfileID(fps)
		assert.Equal(t, "PTSRP7rdCnmvdjRoPMTrtjj7qk7PxR1a9YdBWUGwnZXJh2", id)
	}
}
