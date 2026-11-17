package hub

import (
	"errors"
	"testing"
	"time"

	"github.com/safing/jess"
	"github.com/safing/structures/dsd"
)

// TestHubMsgsRejectCompressed checks that gzip compressed hub messages (and
// compressed payloads in correctly signed hub messages) are rejected before
// decompression. dsd.Load decompresses them into an unbounded buffer, so a few
// KB of gossiped data could make every Hub allocate many MB.
func TestHubMsgsRejectCompressed(t *testing.T) {
	t.Parallel()

	env, hubID := newTestHubEnvelope(t)
	a := &Announcement{
		ID:         hubID,
		Timestamp:  time.Now().Unix(),
		Name:       "compressed",
		Transports: []string{"tcp:1717"},
	}
	s := &Status{
		Timestamp: time.Now().Unix(),
		Version:   "0.1.0",
	}

	// Messages as sent by Hubs are accepted.
	announcementData, err := a.Export(env)
	if err != nil {
		t.Fatal(err)
	}
	statusData, err := s.Export(env)
	if err != nil {
		t.Fatal(err)
	}
	h, _, _, err := ApplyAnnouncement(nil, announcementData, "test-compressed-ok", ScopePublic, false)
	if err != nil {
		t.Fatalf("valid announcement: %s", err)
	}
	_, _, _, err = ApplyStatus(h, statusData, "test-compressed-ok", ScopePublic, false) //nolint:dogsled
	if err != nil {
		t.Fatalf("valid status: %s", err)
	}

	// Compressed letter.
	letter, err := jess.LetterFromDSD(announcementData)
	if err != nil {
		t.Fatal(err)
	}
	compressedLetter, err := dsd.DumpAndCompress(letter, dsd.JSON, dsd.GZIP)
	if err != nil {
		t.Fatal(err)
	}
	_, _, _, err = OpenHubMsg(nil, compressedLetter, "test-compressed-letter", true) //nolint:dogsled
	if !errors.Is(err, dsd.ErrIncompatibleFormat) {
		t.Errorf("compressed letter: expected ErrIncompatibleFormat, got %v", err)
	}

	// Compressed announcement in a correctly signed letter.
	compressedMsg, err := dsd.DumpAndCompress(a, dsd.JSON, dsd.GZIP)
	if err != nil {
		t.Fatal(err)
	}
	data, err := SignHubMsg(compressedMsg, env, true)
	if err != nil {
		t.Fatal(err)
	}
	_, _, _, err = ApplyAnnouncement(nil, data, "test-compressed-announcement", ScopePublic, false) //nolint:dogsled
	if !errors.Is(err, dsd.ErrIncompatibleFormat) {
		t.Errorf("compressed announcement: expected ErrIncompatibleFormat, got %v", err)
	}

	// Compressed status in a correctly signed letter.
	compressedMsg, err = dsd.DumpAndCompress(s, dsd.JSON, dsd.GZIP)
	if err != nil {
		t.Fatal(err)
	}
	data, err = SignHubMsg(compressedMsg, env, false)
	if err != nil {
		t.Fatal(err)
	}
	_, _, _, err = ApplyStatus(h, data, "test-compressed-ok", ScopePublic, false) //nolint:dogsled
	if !errors.Is(err, dsd.ErrIncompatibleFormat) {
		t.Errorf("compressed status: expected ErrIncompatibleFormat, got %v", err)
	}
}
