package windowskext

// This file contains example code how to read bandwidth stats from the kext. Its not ment to be used in production.

import (
	"time"

	"github.com/safing/portbase/log"
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

func StartBandwidthWorker() {
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
				if key.ipv6 {
					log.Debugf("Conn: %d %s:%d %s:%d rx:%d tx:%d", key.protocol, convertIPv6(key.localIP), key.localPort, convertIPv6(key.remoteIP), key.remotePort, value.rx, value.tx)
				} else {
					log.Debugf("Conn: %d %s:%d %s:%d rx:%d tx:%d", key.protocol, convertIPv4(key.localIP), key.localPort, convertIPv4(key.remoteIP), key.remotePort, value.rx, value.tx)
				}

			}

		}
	}()
}
