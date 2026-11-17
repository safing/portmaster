package endpoints

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMakeReasonKeyValuePairs(t *testing.T) {
	t.Parallel()

	ep := &EndpointBase{}
	extra := func(keyval ...any) map[string]any {
		r, ok := ep.makeReason(&EndpointAny{}, "v", "desc", keyval...).(*reason)
		require.True(t, ok, "must be a *reason")
		return r.Extra
	}

	assert.Empty(t, extra())
	assert.Equal(t, map[string]any{"a": 1}, extra("a", 1))
	assert.Equal(
		t,
		map[string]any{"a": 1, "b": "two", "c": true},
		extra("a", 1, "b", "two", "c", true),
	)

	// Odd number of arguments: trailing key without value is ignored.
	assert.NotPanics(t, func() {
		assert.Equal(t, map[string]any{"a": 1}, extra("a", 1, "b"))
		assert.Empty(t, extra("a"))
	})

	// Non-string keys are skipped.
	assert.Equal(t, map[string]any{"b": 2}, extra(1, 1, "b", 2))
}
