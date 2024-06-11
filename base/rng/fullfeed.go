package rng

import (
	"time"

	"github.com/safing/portmaster/service/mgr"
)

func getFullFeedDuration() time.Duration {
	// full feed every 5x time of reseedAfterSeconds
	secsUntilFullFeed := reseedAfterSeconds * 5

	// full feed at most once every ten minutes
	if secsUntilFullFeed < 600 {
		secsUntilFullFeed = 600
	}

	return time.Duration(secsUntilFullFeed) * time.Second
}

func fullFeeder(ctx *mgr.WorkerCtx) error {
	fullFeedDuration := getFullFeedDuration()

	for {
		select {
		case <-time.After(fullFeedDuration):

			rngLock.Lock()
		feedAll:
			for {
				select {
				case data := <-rngFeeder:
					rng.Reseed(data)
				default:
					break feedAll
				}
			}
			rngLock.Unlock()

		case <-ctx.Done():
			return nil
		}
	}
}
