package network

import (
	"sync"
	"testing"
	"time"

	"github.com/tevino/abool"
)

// TestDNSRequestAttributionRace checks that the data of a saved dns request
// connection can be read while the connection is changed by its packet
// handler worker and the connection cleaner. Run with -race.
func TestDNSRequestAttributionRace(t *testing.T) {
	t.Parallel()

	conn := &Connection{ID: "test-dns-request", PID: 1234, Started: time.Now().Unix()}

	var wg sync.WaitGroup
	wg.Go(func() {
		for i := range 1000 {
			// Like fastTrackedPermit and the connection cleaner.
			conn.Lock()
			conn.PID = 1000 + i
			conn.Ended = int64(i)
			conn.Unlock()
		}
	})

	for range 1000 {
		pid, _, _, description := conn.dnsRequestAttribution()
		if pid < 1000 || description != conn.ID {
			t.Errorf("unexpected attribution: pid=%d description=%q", pid, description)
			break
		}
	}
	wg.Wait()
}

// TestIsAttributedToProfileRace checks that the profile of a connection can
// be checked while the connection is re-attributed. Run with -race.
func TestIsAttributedToProfileRace(t *testing.T) {
	t.Parallel()

	const (
		source  = "local"
		profile = "profile-a"
	)

	conn := &Connection{
		ID:           "test-attribution",
		dataComplete: abool.NewBool(true),
		ProcessContext: ProcessContext{
			Source:  source,
			Profile: profile,
		},
	}

	var wg sync.WaitGroup
	wg.Go(func() {
		for i := range 1000 {
			// Like reAttributeConnection.
			conn.Lock()
			if i%2 == 0 {
				conn.ProcessContext = ProcessContext{Source: source, Profile: "profile-with-a-longer-id-b"}
			} else {
				conn.ProcessContext = ProcessContext{Source: source, Profile: profile}
			}
			conn.Unlock()
		}
	})

	for range 1000 {
		if conn.IsAttributedToProfile(source, "other") {
			t.Error("connection must not match another profile")
			break
		}
	}
	wg.Wait()

	if !conn.IsAttributedToProfile(source, profile) {
		t.Error("connection must match its profile")
	}
	if conn.IsAttributedToProfile("special", profile) {
		t.Error("connection must not match another profile source")
	}

	incomplete := &Connection{
		dataComplete:   abool.NewBool(false),
		ProcessContext: ProcessContext{Source: source, Profile: profile},
	}
	if incomplete.IsAttributedToProfile(source, profile) {
		t.Error("incomplete connection must not match")
	}
}
