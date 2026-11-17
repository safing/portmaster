package nameserver

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/miekg/dns"

	"github.com/safing/portmaster/service/resolver"
)

func TestCheckIfQueryIsFailingNoRace(t *testing.T) { //nolint:paralleltest // Replaces the package-level failing queries map.
	q := &resolver.Query{FQDN: "failing.example.com.", QType: dns.Type(dns.TypeA)}
	failErr := errors.New("failed") //nolint:err113 // Test error.

	failingQueriesLock.Lock()
	failingQueries = make(map[string]*failingQuery)
	failingQueriesLock.Unlock()
	addFailingQuery(q, failErr)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for range 1000 {
			addFailingQuery(q, failErr)
		}
	}()
	go func() {
		defer wg.Done()
		for range 1000 {
			until, err := checkIfQueryIsFailing(q)
			if until != nil {
				// Read outside of any lock, as the real caller does.
				_ = until.After(time.Now())
				if err == nil {
					t.Error("expected error with failing until")
				}
			}
		}
	}()
	wg.Wait()
}
