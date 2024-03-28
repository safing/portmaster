package navigator

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStates(t *testing.T) {
	t.Parallel()

	p := &Pin{}

	p.addStates(StateInvalid | StateFailing | StateSuperseded)
	assert.Equal(t, StateInvalid|StateFailing|StateSuperseded, p.State)

	p.removeStates(StateFailing | StateSuperseded)
	assert.Equal(t, StateInvalid, p.State)

	p.addStates(StateTrusted | StateActive)
	assert.True(t, p.State.Has(StateInvalid|StateTrusted))
	assert.False(t, p.State.Has(StateInvalid|StateSuperseded))
	assert.True(t, p.State.HasAnyOf(StateInvalid|StateTrusted))
	assert.True(t, p.State.HasAnyOf(StateInvalid|StateSuperseded))
	assert.False(t, p.State.HasAnyOf(StateSuperseded|StateFailing))

	assert.False(t, p.State.Has(StateSummaryRegard))
	assert.False(t, p.State.Has(StateSummaryDisregard))
	assert.True(t, p.State.HasAnyOf(StateSummaryRegard))
	assert.True(t, p.State.HasAnyOf(StateSummaryDisregard))
}
