package metrics

import (
	"runtime"
	"strings"
	"sync/atomic"

	"github.com/safing/portmaster/base/info"
)

var reportedStart atomic.Bool

func registerInfoMetric() error {
	meta := info.GetInfo()
	_, err := NewGauge(
		"info",
		map[string]string{
			"version":      checkUnknown(meta.Version),
			"commit":       checkUnknown(meta.Commit),
			"build_date":   checkUnknown(meta.BuildTime),
			"build_source": checkUnknown(meta.Source),
			"go_os":        runtime.GOOS,
			"go_arch":      runtime.GOARCH,
			"go_version":   runtime.Version(),
			"go_compiler":  runtime.Compiler,
			"comment":      commentOption(),
		},
		func() float64 {
			// Report as 0 the first time in order to detect (re)starts.
			if reportedStart.CompareAndSwap(false, true) {
				return 0
			}
			return 1
		},
		nil,
	)
	return err
}

func checkUnknown(s string) string {
	if strings.Contains(s, "unknown") {
		return "unknown"
	}
	return s
}
