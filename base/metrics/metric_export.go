package metrics

import (
	"github.com/safing/portmaster/base/api"
)

// UIntMetric is an interface for special functions of uint metrics.
type UIntMetric interface {
	CurrentValue() uint64
}

// FloatMetric is an interface for special functions of float metrics.
type FloatMetric interface {
	CurrentValue() float64
}

// MetricExport is used to export a metric and its current value.
type MetricExport struct {
	Metric
	CurrentValue any
}

// ExportMetrics exports all registered metrics.
func ExportMetrics(requestPermission api.Permission) []*MetricExport {
	registryLock.RLock()
	defer registryLock.RUnlock()

	export := make([]*MetricExport, 0, len(registry))
	for _, metric := range registry {
		// Check permission.
		if requestPermission < metric.Opts().Permission {
			continue
		}

		// Add metric with current value.
		export = append(export, &MetricExport{
			Metric:       metric,
			CurrentValue: getCurrentValue(metric),
		})
	}

	return export
}

// ExportValues exports the values of all supported metrics.
func ExportValues(requestPermission api.Permission, internalOnly bool) map[string]any {
	registryLock.RLock()
	defer registryLock.RUnlock()

	export := make(map[string]any, len(registry))
	for _, metric := range registry {
		// Check permission.
		if requestPermission < metric.Opts().Permission {
			continue
		}

		// Get Value.
		v := getCurrentValue(metric)
		if v == nil {
			continue
		}

		// Get ID.
		var id string
		switch {
		case metric.Opts().InternalID != "":
			id = metric.Opts().InternalID
		case internalOnly:
			continue
		default:
			id = metric.LabeledID()
		}

		// Add to export
		export[id] = v
	}

	return export
}

func getCurrentValue(metric Metric) any {
	if m, ok := metric.(UIntMetric); ok {
		return m.CurrentValue()
	}
	if m, ok := metric.(FloatMetric); ok {
		return m.CurrentValue()
	}
	return nil
}
