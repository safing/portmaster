package hub

import (
	"testing"
	"time"
)

// TestApplyStatusWithNilEntries checks that a status with null entries in its
// keys or lanes is rejected with an error instead of a panic.
func TestApplyStatusWithNilEntries(t *testing.T) {
	t.Parallel()

	env, hubID := newTestHubEnvelope(t)
	a := &Announcement{
		ID:         hubID,
		Timestamp:  time.Now().Unix(),
		Name:       "test",
		Transports: []string{"tcp:17"},
	}
	data, err := a.Export(env)
	if err != nil {
		t.Fatal(err)
	}
	h, _, _, err := ApplyAnnouncement(nil, data, "test-nil-entries", ScopeTest, false)
	if err != nil {
		t.Fatal(err)
	}

	for i, s := range []*Status{
		{Timestamp: time.Now().Unix() + 1, Keys: map[string]*Key{"key": nil}},
		{Timestamp: time.Now().Unix() + 2, Lanes: []*Lane{nil}},
	} {
		data, err := s.Export(env)
		if err != nil {
			t.Fatal(err)
		}
		_, _, _, err = ApplyStatus(h, data, "test-nil-entries", ScopeTest, false)
		if err == nil {
			t.Fatalf("status %d with nil entry was accepted", i)
		}
		if !h.InvalidStatus {
			t.Fatalf("status %d with nil entry was not marked invalid", i)
		}
		if h.Status != nil {
			t.Fatalf("status %d with nil entry was saved", i)
		}
	}
}
