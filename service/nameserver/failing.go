package nameserver

import (
	"sync"
	"time"

	"github.com/safing/portmaster/service/netenv"
	"github.com/safing/portmaster/service/resolver"
)

type failingQuery struct {
	// Until specifies until when the query should be regarded as failing.
	Until time.Time

	// Keep specifies until when the failing status shall be kept.
	Keep time.Time

	// Times specifies how often this query failed.
	Times int

	// Err holds the error the query failed with.
	Err error
}

const (
	failingDelay             = 900 * time.Millisecond
	failingBaseDuration      = 900 * time.Millisecond
	failingFactorDuration    = 500 * time.Millisecond
	failingMaxDuration       = 30 * time.Second
	failingKeepAddedDuration = 10 * time.Second
)

var (
	failingQueries                   = make(map[string]*failingQuery)
	failingQueriesLock               sync.RWMutex
	failingQueriesNetworkChangedFlag = netenv.GetNetworkChangedFlag()
)

func checkIfQueryIsFailing(q *resolver.Query) (failingUntil *time.Time, failingErr error) {
	// If the network changed, reset the failed queries.
	if failingQueriesNetworkChangedFlag.IsSet() {
		failingQueriesNetworkChangedFlag.Refresh()

		failingQueriesLock.Lock()
		defer failingQueriesLock.Unlock()

		// Compiler optimized map reset.
		for key := range failingQueries {
			delete(failingQueries, key)
		}

		return nil, nil
	}

	failingQueriesLock.RLock()
	defer failingQueriesLock.RUnlock()

	// Quickly return if map is empty.
	if len(failingQueries) == 0 {
		return nil, nil
	}

	// Check if query failed recently.
	failing, ok := failingQueries[q.ID()]
	if !ok {
		return nil, nil
	}

	// Check if failing query should still be regarded as failing.
	if time.Now().After(failing.Until) {
		return nil, nil
	}

	// Return failing error and until when it's valid.
	return &failing.Until, failing.Err
}

func addFailingQuery(q *resolver.Query, err error) {
	// Check if we were given an error.
	if err == nil {
		return
	}

	// Exclude reverse and mDNS queries, as they fail _often_ and are usually not
	// retried quickly.
	// if strings.HasSuffix(q.FQDN, ".in-addr.arpa.") ||
	// 	strings.HasSuffix(q.FQDN, ".ip6.arpa.") ||
	// 	strings.HasSuffix(q.FQDN, ".local.") {
	// 	return
	// }

	failingQueriesLock.Lock()
	defer failingQueriesLock.Unlock()

	failing, ok := failingQueries[q.ID()]
	if !ok {
		failing = &failingQuery{Err: err}
		failingQueries[q.ID()] = failing
	}

	// Calculate fail duration.
	// Initial fail duration will be at 900ms, perfect for a normal retry after 1s,
	// but not any earlier.
	failDuration := failingBaseDuration + time.Duration(failing.Times)*failingFactorDuration
	if failDuration > failingMaxDuration {
		failDuration = failingMaxDuration
	}

	// Update failing query.
	failing.Times++
	failing.Until = time.Now().Add(failDuration)
	failing.Keep = failing.Until.Add(failingKeepAddedDuration)
}

func cleanFailingQueries(maxRemove, maxMiss int) {
	failingQueriesLock.Lock()
	defer failingQueriesLock.Unlock()

	now := time.Now()
	for key, failing := range failingQueries {
		if now.After(failing.Keep) {
			delete(failingQueries, key)

			maxRemove--
			if maxRemove == 0 {
				return
			}
		} else {
			maxMiss--
			if maxMiss == 0 {
				return
			}
		}
	}
}
