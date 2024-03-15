package terminal

import "time"

// RateLimiter is a data flow rate limiter.
type RateLimiter struct {
	maxBytesPerSlot uint64
	slotBytes       uint64
	slotStarted     time.Time
}

// NewRateLimiter returns a new rate limiter.
// The given MBit/s are transformed to bytes, so giving a multiple of 8 is
// advised for accurate results.
func NewRateLimiter(mbits uint64) *RateLimiter {
	return &RateLimiter{
		maxBytesPerSlot: (mbits / 8) * 1_000_000,
		slotStarted:     time.Now(),
	}
}

// Limit is given the current transferred bytes and blocks until they may be sent.
func (rl *RateLimiter) Limit(xferBytes uint64) {
	// Check if we need to limit transfer if we go over to max bytes per slot.
	if rl.slotBytes > rl.maxBytesPerSlot {
		// Wait if we are still within the slot.
		sinceSlotStart := time.Since(rl.slotStarted)
		if sinceSlotStart < time.Second {
			time.Sleep(time.Second - sinceSlotStart)
		}

		// Reset state for next slot.
		rl.slotBytes = 0
		rl.slotStarted = time.Now()
	}

	// Add new bytes after checking, as first step over the limit is fully using the limit.
	rl.slotBytes += xferBytes
}
