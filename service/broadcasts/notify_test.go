package broadcasts

import (
	"errors"
	"testing"

	"github.com/safing/portmaster/base/database/accessor"
)

func TestParseBroadcastSourceEmptyEntry(t *testing.T) {
	t.Parallel()

	// An entry without any content is decoded as nil.
	_, err := parseBroadcastSource([]byte("notifications:\n  test1:\n"))
	if err == nil {
		t.Fatal("expected error for empty broadcast notification")
	}
}

func TestHandleBroadcastWithoutNotification(t *testing.T) {
	t.Parallel()

	// An entry without any notification fields has no notification.
	broadcasts, err := parseBroadcastSource([]byte("notifications:\n  test1:\n    permanent: true\n"))
	if err != nil {
		t.Fatalf("failed to parse: %s", err)
	}
	err = handleBroadcast(broadcasts.Notifications["test1"], nil, nil)
	if err == nil || errors.Is(err, ErrSkip) {
		t.Fatalf("expected failure for broadcast without notification, got %v", err)
	}
}

func FuzzParseBroadcastSource(f *testing.F) {
	f.Add([]byte("notifications:\n  test1:\n    title: a\n    match: \"NumericVersion > 8000\"\n    repeat: 1m\n"))
	f.Add([]byte("notifications:\n  test1:\n    remove: true\n"))
	f.Add([]byte(`{"notifications":{"a":{"match":"Version sameas 0.9.0 and Updates.Channel sameas beta"}}}`))

	matchingJSON := []byte(`{"Version":"1.2.3","NumericVersion":1002003,"Updates":{"Channel":"stable"},"Current":{"UTC":{"NumericDate":20260101}}}`)
	matchingDataAccessor := accessor.NewJSONBytesAccessor(&matchingJSON)

	f.Fuzz(func(t *testing.T, data []byte) {
		broadcasts, err := parseBroadcastSource(data)
		if err != nil {
			return
		}
		for _, bn := range broadcasts.Notifications {
			if bn.matchingQuery != nil {
				bn.matchingQuery.MatchesAccessor(matchingDataAccessor)
			}
		}
	})
}
