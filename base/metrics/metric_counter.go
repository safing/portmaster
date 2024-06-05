package metrics

import (
	vm "github.com/VictoriaMetrics/metrics"
)

// Counter is a counter metric.
type Counter struct {
	*metricBase
	*vm.Counter
}

// NewCounter registers a new counter metric.
func NewCounter(id string, labels map[string]string, opts *Options) (*Counter, error) {
	// Ensure that there are options.
	if opts == nil {
		opts = &Options{}
	}

	// Make base.
	base, err := newMetricBase(id, labels, *opts)
	if err != nil {
		return nil, err
	}

	// Create metric struct.
	m := &Counter{
		metricBase: base,
	}

	// Create metric in set
	m.Counter = m.set.NewCounter(m.LabeledID())

	// Register metric.
	err = register(m)
	if err != nil {
		return nil, err
	}

	// Load state.
	m.loadState()

	return m, nil
}

// CurrentValue returns the current counter value.
func (c *Counter) CurrentValue() uint64 {
	return c.Get()
}
