package interception

import (
	"flag"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/safing/portmaster/base/log"
)

var (
	packetMetricsDestination string
	metrics                  = &packetMetrics{
		done: make(chan struct{}),
	}
)

func init() {
	flag.StringVar(&packetMetricsDestination, "write-packet-metrics", "", "write packet metrics to the specified file")
}

type (
	performanceRecord struct {
		start    int64
		duration time.Duration
		verdict  string
	}

	packetMetrics struct {
		done    chan struct{}
		l       sync.Mutex
		records []*performanceRecord
	}
)

func (pm *packetMetrics) record(tp *tracedPacket, verdict string) {
	go func(start int64, duration time.Duration) {
		pm.l.Lock()
		defer pm.l.Unlock()

		pm.records = append(pm.records, &performanceRecord{
			start:    start,
			duration: duration,
			verdict:  verdict,
		})
	}(tp.start.UnixNano(), time.Since(tp.start))
}

func (pm *packetMetrics) writeMetrics() {
	if packetMetricsDestination == "" {
		return
	}

	f, err := os.Create(packetMetricsDestination)
	if err != nil {
		log.Errorf("Failed to create packet metrics file: %s", err)
		return
	}
	defer func() {
		_ = f.Close()
	}()

	for {
		select {
		case <-pm.done:
			return
		case <-time.After(time.Second * 5):
		}
		pm.l.Lock()
		records := pm.records
		pm.records = nil
		pm.l.Unlock()

		for _, r := range records {
			fmt.Fprintf(f, "%d;%s;%s;%.2f\n", r.start, r.verdict, r.duration, float64(r.duration)/float64(time.Microsecond))
		}
	}
}
