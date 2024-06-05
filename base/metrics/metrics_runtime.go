package metrics

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"strings"

	vm "github.com/VictoriaMetrics/metrics"

	"github.com/safing/portmaster/base/api"
	"github.com/safing/portmaster/base/config"
	"github.com/safing/portmaster/base/log"
)

func registerRuntimeMetric() error {
	runtimeBase, err := newMetricBase("_runtime", nil, Options{
		Name:           "Golang Runtime",
		Permission:     api.PermitAdmin,
		ExpertiseLevel: config.ExpertiseLevelDeveloper,
	})
	if err != nil {
		return err
	}

	return register(&runtimeMetrics{
		metricBase: runtimeBase,
	})
}

type runtimeMetrics struct {
	*metricBase
}

func (r *runtimeMetrics) WritePrometheus(w io.Writer) {
	// If there nothing to change, just write directly to w.
	if metricNamespace == "" && len(globalLabels) == 0 {
		vm.WriteProcessMetrics(w)
		return
	}

	// Write metrics to buffer.
	buf := new(bytes.Buffer)
	vm.WriteProcessMetrics(buf)

	// Add namespace and label per line.
	scanner := bufio.NewScanner(buf)
	scanner.Split(bufio.ScanLines)
	for scanner.Scan() {
		line := scanner.Text()

		// Add namespace, if set.
		if metricNamespace != "" {
			line = metricNamespace + "_" + line
		}

		// Add global labels, if set.
		if len(globalLabels) > 0 {
			// Find where to insert.
			mergeWithExisting := true
			insertAt := strings.Index(line, "{") + 1
			if insertAt <= 0 {
				mergeWithExisting = false
				insertAt = strings.Index(line, " ")
				if insertAt < 0 {
					continue
				}
			}

			// Write new line directly to w.
			fmt.Fprint(w, line[:insertAt])
			if !mergeWithExisting {
				fmt.Fprint(w, "{")
			}
			labelsAdded := 0
			for labelKey, labelValue := range globalLabels {
				fmt.Fprintf(w, "%s=%q", labelKey, labelValue)
				// Add separator if not last label.
				labelsAdded++
				if labelsAdded < len(globalLabels) {
					fmt.Fprint(w, ", ")
				}
			}
			if mergeWithExisting {
				fmt.Fprint(w, ", ")
			} else {
				fmt.Fprint(w, "}")
			}
			fmt.Fprintln(w, line[insertAt:])
		}
	}

	// Check if there was an error in the scanner.
	if scanner.Err() != nil {
		log.Warningf("metrics: failed to scan go process metrics: %s", scanner.Err())
	}
}
