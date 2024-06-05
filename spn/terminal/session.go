package terminal

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/safing/portmaster/base/log"
)

const (
	rateLimitMinOps          = 250
	rateLimitMaxOpsPerSecond = 5

	rateLimitMinSuspicion          = 25
	rateLimitMinPermaSuspicion     = rateLimitMinSuspicion * 100
	rateLimitMaxSuspicionPerSecond = 1

	// Make this big enough to trigger suspicion limit in first blast.
	concurrencyPoolSize = 30
)

// Session holds terminal metadata for operations.
type Session struct {
	sync.RWMutex

	// Rate Limiting.

	// started holds the unix timestamp in seconds when the session was started.
	// It is set when the Session is created and may be treated as a constant.
	started int64

	// opCount is the amount of operations started (and not rate limited by suspicion).
	opCount atomic.Int64

	// suspicionScore holds a score of suspicious activity.
	// Every suspicious operations is counted as at least 1.
	// Rate limited operations because of suspicion are also counted as 1.
	suspicionScore atomic.Int64

	concurrencyPool chan struct{}
}

// SessionTerminal is an interface for terminals that support authorization.
type SessionTerminal interface {
	GetSession() *Session
}

// SessionAddOn can be inherited by terminals to add support for sessions.
type SessionAddOn struct {
	lock sync.Mutex

	// session holds the terminal session.
	session *Session
}

// GetSession returns the terminal's session.
func (t *SessionAddOn) GetSession() *Session {
	t.lock.Lock()
	defer t.lock.Unlock()

	// Create session if it does not exist.
	if t.session == nil {
		t.session = NewSession()
	}

	return t.session
}

// NewSession returns a new session.
func NewSession() *Session {
	return &Session{
		started:         time.Now().Unix() - 1, // Ensure a 1 second difference to current time.
		concurrencyPool: make(chan struct{}, concurrencyPoolSize),
	}
}

// RateLimitInfo returns some basic information about the status of the rate limiter.
func (s *Session) RateLimitInfo() string {
	secondsActive := time.Now().Unix() - s.started

	return fmt.Sprintf(
		"%do/s %ds/s %ds",
		s.opCount.Load()/secondsActive,
		s.suspicionScore.Load()/secondsActive,
		secondsActive,
	)
}

// RateLimit enforces a rate and suspicion limit.
func (s *Session) RateLimit() *Error {
	secondsActive := time.Now().Unix() - s.started

	// Check the suspicion limit.
	score := s.suspicionScore.Load()
	if score > rateLimitMinSuspicion {
		scorePerSecond := score / secondsActive
		if scorePerSecond >= rateLimitMaxSuspicionPerSecond {
			// Add current try to suspicion score.
			s.suspicionScore.Add(1)

			return ErrRateLimited
		}

		// Permanently rate limit if suspicion goes over the perma min limit and
		// the suspicion score is greater than 80% of the operation count.
		if score > rateLimitMinPermaSuspicion &&
			score*5 > s.opCount.Load()*4 { // Think: 80*5 == 100*4
			return ErrRateLimited
		}
	}

	// Check the rate limit.
	count := s.opCount.Add(1)
	if count > rateLimitMinOps {
		opsPerSecond := count / secondsActive
		if opsPerSecond >= rateLimitMaxOpsPerSecond {
			return ErrRateLimited
		}
	}

	return nil
}

// Suspicion Factors.
const (
	SusFactorCommon          = 1
	SusFactorWeirdButOK      = 5
	SusFactorQuiteUnusual    = 10
	SusFactorMustBeMalicious = 100
)

// ReportSuspiciousActivity reports suspicious activity of the terminal.
func (s *Session) ReportSuspiciousActivity(factor int64) {
	s.suspicionScore.Add(factor)
}

// LimitConcurrency limits concurrent executions.
// If over the limit, waiting goroutines are selected randomly.
// It returns the context error if it was canceled.
func (s *Session) LimitConcurrency(ctx context.Context, f func()) error {
	// Wait for place in pool.
	select {
	case <-ctx.Done():
		return ctx.Err()
	case s.concurrencyPool <- struct{}{}:
		// We added our entry to the pool, continue with execution.
	}

	// Drain own spot if pool after execution.
	defer func() {
		select {
		case <-s.concurrencyPool:
			// Own entry drained.
		default:
			// This should never happen, but let's play safe and not deadlock when pool is empty.
			log.Warningf("spn/session: failed to drain own entry from concurrency pool")
		}
	}()

	// Execute and return.
	f()
	return nil
}
