package metrics

import (
	vm "github.com/VictoriaMetrics/metrics"
)

// Gauge is a gauge metric.
type Gauge struct {
	*metricBase
	*vm.Gauge
}

// NewGauge registers a new gauge metric.
func NewGauge(id string, labels map[string]string, fn func() float64, opts *Options) (*Gauge, error) {
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
	m := &Gauge{
		metricBase: base,
	}

	// Create metric in set
	m.Gauge = m.set.NewGauge(m.LabeledID(), fn)

	// Register metric.
	err = register(m)
	if err != nil {
		return nil, err
	}

	return m, nil
}

// CurrentValue returns the current gauge value.
func (g *Gauge) CurrentValue() float64 {
	return g.Get()
}
