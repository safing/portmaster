package runtime

import (
	"time"

	"github.com/safing/portmaster/base/database/record"
	"github.com/safing/portmaster/base/log"
)

// traceValueProvider can be used to wrap an
// existing value provider to trace an calls to
// their Set and Get methods.
type traceValueProvider struct {
	ValueProvider
}

// TraceProvider returns a new ValueProvider that wraps
// vp but traces all Set and Get methods calls.
func TraceProvider(vp ValueProvider) ValueProvider {
	return &traceValueProvider{vp}
}

func (tvp *traceValueProvider) Set(r record.Record) (res record.Record, err error) {
	defer func(start time.Time) {
		log.Tracef("runtime: setting record %q: duration=%s err=%v", r.Key(), time.Since(start), err)
	}(time.Now())

	return tvp.ValueProvider.Set(r)
}

func (tvp *traceValueProvider) Get(keyOrPrefix string) (records []record.Record, err error) {
	defer func(start time.Time) {
		log.Tracef("runtime: loading records %q: duration=%s err=%v #records=%d", keyOrPrefix, time.Since(start), err, len(records))
	}(time.Now())

	return tvp.ValueProvider.Get(keyOrPrefix)
}
