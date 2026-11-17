package log

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// TestWriterSurvivesPanic checks that the log writer keeps processing log
// lines after a panic occurred while writing a line.
func TestWriterSurvivesPanic(t *testing.T) {
	t.Parallel()

	// Submit a trace whose remaining trace lines contain a nil entry. Formatting
	// this line panics inside the writer goroutine.
	poisoned := &ContextTracer{
		logs: []*logLine{
			nil,
			{
				msg:       "poisoned trace",
				level:     InfoLevel,
				timestamp: time.Now(),
			},
		},
	}
	poisoned.Submit()

	// Keep logging a warning until the writer processed one of them. Lines
	// that are processed together with the poisoned line are lost, so a single
	// warning is not enough.
	marker := fmt.Sprintf("writer-survives-panic-%d", time.Now().UnixNano())
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		Warning(marker)
		time.Sleep(10 * time.Millisecond)
		for _, line := range GetLastUnexpectedLogs() {
			if strings.Contains(line, marker) {
				return
			}
		}
	}
	t.Fatal("log writer stopped processing log lines after a panic")
}
