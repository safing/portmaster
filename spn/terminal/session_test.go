package terminal

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestRateLimit(t *testing.T) {
	t.Parallel()

	var tErr *Error
	s := NewSession()

	// Everything should be okay within the min limit.
	for range rateLimitMinOps {
		tErr = s.RateLimit()
		if tErr != nil {
			t.Error("should not rate limit within min limit")
		}
	}

	// Somewhere here we should rate limiting.
	for range rateLimitMaxOpsPerSecond {
		tErr = s.RateLimit()
	}
	assert.ErrorIs(t, tErr, ErrRateLimited, "should rate limit")
}

func TestSuspicionLimit(t *testing.T) {
	t.Parallel()

	var tErr *Error
	s := NewSession()

	// Everything should be okay within the min limit.
	for range rateLimitMinSuspicion {
		tErr = s.RateLimit()
		if tErr != nil {
			t.Error("should not rate limit within min limit")
		}
		s.ReportSuspiciousActivity(SusFactorCommon)
	}

	// Somewhere here we should rate limiting.
	for range rateLimitMaxSuspicionPerSecond {
		s.ReportSuspiciousActivity(SusFactorCommon)
		tErr = s.RateLimit()
	}
	if tErr == nil {
		t.Error("should rate limit")
	}
}

func TestConcurrencyLimit(t *testing.T) {
	t.Parallel()

	s := NewSession()
	started := time.Now()
	wg := sync.WaitGroup{}
	workTime := 1 * time.Millisecond
	workers := concurrencyPoolSize * 10

	// Start many workers to test concurrency.
	wg.Add(workers)
	for workerNum := range workers {
		go func() {
			defer func() {
				_ = recover()
			}()
			_ = s.LimitConcurrency(context.Background(), func() {
				time.Sleep(workTime)
				wg.Done()

				// Panic sometimes.
				if workerNum%concurrencyPoolSize == 0 {
					panic("test")
				}
			})
		}()
	}

	// Wait and check time needed.
	wg.Wait()
	if time.Since(started) < (time.Duration(workers) * workTime / concurrencyPoolSize) {
		t.Errorf("workers were too quick - only took %s", time.Since(started))
	} else {
		t.Logf("workers were correctly limited - took %s", time.Since(started))
	}
}
