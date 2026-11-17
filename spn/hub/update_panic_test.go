package hub

import (
	"testing"
	"time"

	"github.com/safing/jess"
)

func newTestHubEnvelope(tb testing.TB) (*jess.Envelope, string) {
	tb.Helper()

	private, _, err := CreateHubSignet("Ed25519", 0)
	if err != nil {
		tb.Fatal(err)
	}

	env := jess.NewUnconfiguredEnvelope()
	env.SuiteID = jess.SuiteSignV1
	env.Senders = []*jess.Signet{private}
	return env, private.ID
}

// TestApplyInvalidAnnouncementOfNewHub checks that an invalid announcement of
// a previously unknown Hub is rejected with an error instead of a panic.
func TestApplyInvalidAnnouncementOfNewHub(t *testing.T) {
	t.Parallel()

	env, hubID := newTestHubEnvelope(t)
	a := &Announcement{
		ID:         hubID,
		Timestamp:  time.Now().Unix(),
		Name:       "invalid$name",
		Transports: []string{"tcp:17"},
	}
	data, err := a.Export(env)
	if err != nil {
		t.Fatal(err)
	}

	h, known, _, err := ApplyAnnouncement(nil, data, "test-invalid-new", ScopePublic, false)
	if err == nil {
		t.Fatal("invalid announcement was accepted")
	}
	if known {
		t.Fatal("hub should not be known")
	}
	if h == nil || h.Info != nil {
		t.Fatal("expected unknown hub without info")
	}
	// The name must also be safe to use for such a hub.
	_ = h.Name()
	_ = h.String()
}
