package resolver

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/safing/portmaster/netenv"

	"github.com/miekg/dns"

	"github.com/safing/portbase/database"
	"github.com/safing/portbase/log"
)

var (
	// basic errors

	// ErrNotFound is a basic error that will match all "not found" errors
	ErrNotFound = errors.New("record does not exist")
	// ErrBlocked is basic error that will match all "blocked" errors
	ErrBlocked = errors.New("query was blocked")
	// ErrLocalhost is returned to *.localhost queries
	ErrLocalhost = errors.New("query for localhost")
	// ErrTimeout is returned when a query times out
	ErrTimeout = errors.New("query timed out")
	// ErrOffline is returned when no network connection is detected
	ErrOffline = errors.New("device is offine")
	// ErrFailure is returned when the type of failure is unclear
	ErrFailure = errors.New("query failed")

	// detailed errors

	// ErrTestDomainsDisabled wraps ErrBlocked
	ErrTestDomainsDisabled = fmt.Errorf("%w: test domains disabled", ErrBlocked)
	// ErrSpecialDomainsDisabled wraps ErrBlocked
	ErrSpecialDomainsDisabled = fmt.Errorf("%w: special domains disabled", ErrBlocked)
	// ErrInvalid wraps ErrNotFound
	ErrInvalid = fmt.Errorf("%w: invalid request", ErrNotFound)
	// ErrNoCompliance wraps ErrBlocked and is returned when no resolvers were able to comply with the current settings
	ErrNoCompliance = fmt.Errorf("%w: no compliant resolvers for this query", ErrBlocked)
)

// BlockedUpstreamError is returned when a DNS request
// has been blocked by the upstream server.
type BlockedUpstreamError struct {
	ResolverName string
}

func (blocked *BlockedUpstreamError) Error() string {
	return fmt.Sprintf("%s by upstream DNS resolver %s", ErrBlocked, blocked.ResolverName)
}

// Unwrap implements errors.Unwrapper
func (blocked *BlockedUpstreamError) Unwrap() error {
	return ErrBlocked
}

// Query describes a dns query.
type Query struct {
	FQDN               string
	QType              dns.Type
	SecurityLevel      uint8
	NoCaching          bool
	IgnoreFailing      bool
	LocalResolversOnly bool

	// internal
	dotPrefixedFQDN string
}

// check runs sanity checks and does some initialization. Returns whether the query passed the basic checks.
func (q *Query) check() (ok bool) {
	if q.FQDN == "" {
		return false
	}

	// init
	q.FQDN = dns.Fqdn(q.FQDN)
	if q.FQDN == "." {
		q.dotPrefixedFQDN = q.FQDN
	} else {
		q.dotPrefixedFQDN = "." + q.FQDN
	}

	return true
}

// Resolve resolves the given query for a domain and type and returns a RRCache object or nil, if the query failed.
func Resolve(ctx context.Context, q *Query) (rrCache *RRCache, err error) {
	// sanity check
	if q == nil || !q.check() {
		return nil, ErrInvalid
	}

	// log
	log.Tracer(ctx).Tracef("resolver: resolving %s%s", q.FQDN, q.QType)

	// check query compliance
	if err = q.checkCompliance(); err != nil {
		return nil, err
	}

	// check the cache
	if !q.NoCaching {
		rrCache = checkCache(ctx, q)
		if rrCache != nil {
			rrCache.MixAnswers()
			return rrCache, nil
		}

		// dedupe!
		markRequestFinished := deduplicateRequest(ctx, q)
		if markRequestFinished == nil {
			// we waited for another request, recheck the cache!
			rrCache = checkCache(ctx, q)
			if rrCache != nil {
				rrCache.MixAnswers()
				return rrCache, nil
			}
			log.Tracer(ctx).Debugf("resolver: waited for another %s%s query, but cache missed!", q.FQDN, q.QType)
			// if cache is still empty or non-compliant, go ahead and just query
		} else {
			// we are the first!
			defer markRequestFinished()

		}
	}

	return resolveAndCache(ctx, q)
}

func checkCache(ctx context.Context, q *Query) *RRCache {
	rrCache, err := GetRRCache(q.FQDN, q.QType)

	// failed to get from cache
	if err != nil {
		if err != database.ErrNotFound {
			log.Tracer(ctx).Warningf("resolver: getting RRCache %s%s from database failed: %s", q.FQDN, q.QType.String(), err)
		}
		return nil
	}

	// get resolver that rrCache was resolved with
	resolver := getActiveResolverByIDWithLocking(rrCache.Server)
	if resolver == nil {
		log.Tracer(ctx).Debugf("resolver: ignoring RRCache %s%s because source server %s has been removed", q.FQDN, q.QType.String(), rrCache.Server)
		return nil
	}

	// check compliance of resolver
	err = resolver.checkCompliance(ctx, q)
	if err != nil {
		log.Tracer(ctx).Debugf("resolver: cached entry for %s%s does not comply to query parameters: %s", q.FQDN, q.QType.String(), err)
		return nil
	}

	// check if expired
	if rrCache.Expired() {
		rrCache.Lock()
		rrCache.requestingNew = true
		rrCache.Unlock()

		log.Tracer(ctx).Trace("resolver: serving from cache, requesting new")

		// resolve async
		module.StartWorker("resolve async", func(ctx context.Context) error {
			_, _ = resolveAndCache(ctx, q)
			return nil
		})
	}

	log.Tracer(ctx).Tracef("resolver: using cached RR (expires in %s)", time.Until(time.Unix(rrCache.TTL, 0)))
	return rrCache
}

func deduplicateRequest(ctx context.Context, q *Query) (finishRequest func()) {
	// create identifier key
	dupKey := fmt.Sprintf("%s%s", q.FQDN, q.QType.String())

	dupReqLock.Lock()

	// get  duplicate request waitgroup
	wg, requestActive := dupReqMap[dupKey]

	// someone else is already on it!
	if requestActive {
		dupReqLock.Unlock()

		// log that we are waiting
		log.Tracer(ctx).Tracef("resolver: waiting for duplicate query for %s to complete", dupKey)
		// wait
		wg.Wait()
		// done!
		return nil
	}

	// we are currently the only one doing a request for this

	// create new waitgroup
	wg = new(sync.WaitGroup)
	// add worker (us!)
	wg.Add(1)
	// add to registry
	dupReqMap[dupKey] = wg

	dupReqLock.Unlock()

	// return function to mark request as finished
	return func() {
		dupReqLock.Lock()
		defer dupReqLock.Unlock()
		// mark request as done
		wg.Done()
		// delete from registry
		delete(dupReqMap, dupKey)
	}
}

func resolveAndCache(ctx context.Context, q *Query) (rrCache *RRCache, err error) { //nolint:gocognit
	// get resolvers
	resolvers := GetResolversInScope(ctx, q)
	if len(resolvers) == 0 {
		return nil, ErrNoCompliance
	}

	// check if we are online
	if netenv.GetOnlineStatus() == netenv.StatusOffline {
		if !netenv.IsOnlineStatusTestDomain(q.FQDN) {
			log.Tracer(ctx).Debugf("resolver: not resolving %s, device is offline", q.FQDN)
			// we are offline and this is not an online check query
			return nil, ErrOffline
		}
		log.Tracer(ctx).Debugf("resolver: permitting online status test domain %s to resolve even though offline", q.FQDN)
	}

	// start resolving

	var i int
	// once with skipping recently failed resolvers, once without
resolveLoop:
	for i = 0; i < 2; i++ {
		for _, resolver := range resolvers {
			// check if resolver failed recently (on first run)
			if i == 0 && resolver.Conn.IsFailing() {
				log.Tracer(ctx).Tracef("resolver: skipping resolver %s, because it failed recently", resolver)
				continue
			}

			// resolve
			rrCache, err = resolver.Conn.Query(ctx, q)
			if err != nil {
				switch {
				case errors.Is(err, ErrNotFound):
					// NXDomain, or similar
					return nil, err
				case errors.Is(err, ErrBlocked):
					// some resolvers might also block
					return nil, err
				case netenv.GetOnlineStatus() == netenv.StatusOffline &&
					!netenv.IsOnlineStatusTestDomain(q.FQDN):
					log.Tracer(ctx).Debugf("resolver: not resolving %s, device is offline", q.FQDN)
					// we are offline and this is not an online check query
					return nil, ErrOffline
				default:
					log.Tracer(ctx).Debugf("resolver: failed to resolve %s: %s", q.FQDN, err)
				}
			} else {
				// no error
				if rrCache == nil {
					// defensive: assume NXDomain
					return nil, ErrNotFound
				}
				break resolveLoop
			}
		}
	}

	// tried all resolvers, possibly twice
	if i > 1 {
		return nil, fmt.Errorf("all %d query-compliant resolvers failed, last error: %s", len(resolvers), err)
	}

	// check for error
	if err != nil {
		return nil, err
	}

	// check for result
	if rrCache == nil /* defensive */ {
		return nil, ErrNotFound
	}

	// cache if enabled
	if !q.NoCaching {
		// persist to database
		rrCache.Clean(600)
		err = rrCache.Save()
		if err != nil {
			log.Warningf("resolver: failed to cache RR for %s%s: %s", q.FQDN, q.QType.String(), err)
		}
	}

	return rrCache, nil
}
