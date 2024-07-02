package metrics

import (
	"fmt"
	"io"

	vm "github.com/VictoriaMetrics/metrics"
)

// FetchingCounter is a counter metric that fetches the values via a function call.
type FetchingCounter struct {
	*metricBase
	counter  *vm.Counter
	fetchCnt func() uint64
}

// NewFetchingCounter registers a new fetching counter metric.
func NewFetchingCounter(id string, labels map[string]string, fn func() uint64, opts *Options) (*FetchingCounter, error) {
	// Check if a fetch function is provided.
	if fn == nil {
		return nil, fmt.Errorf("%w: no fetch function provided", ErrInvalidOptions)
	}

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
	m := &FetchingCounter{
		metricBase: base,
		fetchCnt:   fn,
	}

	// Create metric in set
	m.counter = m.set.NewCounter(m.LabeledID())

	// Register metric.
	err = register(m)
	if err != nil {
		return nil, err
	}

	return m, nil
}

// CurrentValue returns the current counter value.
func (fc *FetchingCounter) CurrentValue() uint64 {
	return fc.fetchCnt()
}

// WritePrometheus writes the metric in the prometheus format to the given writer.
func (fc *FetchingCounter) WritePrometheus(w io.Writer) {
	fc.counter.Set(fc.fetchCnt())
	fc.metricBase.set.WritePrometheus(w)
}
