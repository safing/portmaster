package metrics

import (
	"bytes"
	"strings"
	"testing"
)

func TestRuntimeMetricsWithNamespaceOnly(t *testing.T) { //nolint:paralleltest // Modifies package globals.
	registryLock.Lock()
	prevNamespace, prevLabels := metricNamespace, globalLabels
	metricNamespace = "testns"
	globalLabels = make(map[string]string)
	registryLock.Unlock()
	t.Cleanup(func() {
		registryLock.Lock()
		metricNamespace, globalLabels = prevNamespace, prevLabels
		registryLock.Unlock()
	})

	buf := &bytes.Buffer{}
	(&runtimeMetrics{}).WritePrometheus(buf)

	if buf.Len() == 0 {
		t.Fatal("no runtime metrics written when only a namespace is set")
	}
	for line := range strings.Lines(buf.String()) {
		if !strings.HasPrefix(line, "testns_") {
			t.Fatalf("runtime metric line without namespace: %q", line)
		}
	}
}
