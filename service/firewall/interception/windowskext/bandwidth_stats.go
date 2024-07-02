//go:build windows
// +build windows

package windowskext

// This file contains example code how to read bandwidth stats from the kext. Its not ment to be used in production.

import (
	"context"
	"time"

	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/service/network/packet"
)

type Rxtxdata struct {
	rx uint64
	tx uint64
}

type Key struct {
	localIP    [4]uint32
	remoteIP   [4]uint32
	localPort  uint16
	remotePort uint16
	ipv6       bool
	protocol   uint8
}

var m = make(map[Key]Rxtxdata)

func BandwidthStatsWorker(ctx context.Context, collectInterval time.Duration, bandwidthUpdates chan *packet.BandwidthUpdate) error {
	// Setup ticker.
	ticker := time.NewTicker(collectInterval)
	defer ticker.Stop()

	// Collect bandwidth at every tick.
	for {
		select {
		case <-ticker.C:
			err := reportBandwidth(ctx, bandwidthUpdates)
			if err != nil {
				return err
			}
		case <-ctx.Done():
			return nil
		}
	}
}

func reportBandwidth(ctx context.Context, bandwidthUpdates chan *packet.BandwidthUpdate) error {
	stats, err := GetConnectionsStats()
	if err != nil {
		return err
	}

	// Report all statistics.
	for i, stat := range stats {
		connID := packet.CreateConnectionID(
			packet.IPProtocol(stat.protocol),
			convertArrayToIP(stat.localIP, stat.ipV6 == 1), stat.localPort,
			convertArrayToIP(stat.remoteIP, stat.ipV6 == 1), stat.remotePort,
			false,
		)
		update := &packet.BandwidthUpdate{
			ConnID:        connID,
			BytesReceived: stat.receivedBytes,
			BytesSent:     stat.transmittedBytes,
			Method:        packet.Additive,
		}
		select {
		case bandwidthUpdates <- update:
		case <-ctx.Done():
			return nil
		default:
			log.Warningf("kext: bandwidth update queue is full, skipping rest of batch (%d entries)", len(stats)-i)
			return nil
		}
	}

	return nil
}

func StartBandwidthConsoleLogger() {
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			conns, err := GetConnectionsStats()
			if err != nil {
				continue
			}
			for _, conn := range conns {
				if conn.receivedBytes == 0 && conn.transmittedBytes == 0 {
					continue
				}
				key := Key{
					localIP:    conn.localIP,
					remoteIP:   conn.remoteIP,
					localPort:  conn.localPort,
					remotePort: conn.remotePort,
					ipv6:       conn.ipV6 == 1,
					protocol:   conn.protocol,
				}

				// First we get a "copy" of the entry
				if entry, ok := m[key]; ok {
					// Then we modify the copy
					entry.rx += conn.receivedBytes
					entry.tx += conn.transmittedBytes

					// Then we reassign map entry
					m[key] = entry
				} else {
					m[key] = Rxtxdata{
						rx: conn.receivedBytes,
						tx: conn.transmittedBytes,
					}
				}
			}
			log.Debug("----------------------------------")
			for key, value := range m {
				log.Debugf(
					"Conn: %d %s:%d %s:%d rx:%d tx:%d", key.protocol,
					convertArrayToIP(key.localIP, key.ipv6), key.localPort,
					convertArrayToIP(key.remoteIP, key.ipv6), key.remotePort,
					value.rx, value.tx,
				)
			}
		}
	}()
}
