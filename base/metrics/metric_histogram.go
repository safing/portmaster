package metrics

import (
	vm "github.com/VictoriaMetrics/metrics"
)

// Histogram is a histogram metric.
type Histogram struct {
	*metricBase
	*vm.Histogram
}

// NewHistogram registers a new histogram metric.
func NewHistogram(id string, labels map[string]string, opts *Options) (*Histogram, error) {
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
	m := &Histogram{
		metricBase: base,
	}

	// Create metric in set
	m.Histogram = m.set.NewHistogram(m.LabeledID())

	// Register metric.
	err = register(m)
	if err != nil {
		return nil, err
	}

	return m, nil
}
