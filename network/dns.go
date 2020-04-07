package network

import (
	"context"
	"strconv"
	"sync"
	"time"
)

var (
	openDNSRequests     = make(map[string]*Connection) // key: <pid>/fqdn
	openDNSRequestsLock sync.Mutex

	// write open dns requests every
	writeOpenDNSRequestsTickDuration = 5 * time.Second

	// duration after which DNS requests without a following connection are logged
	openDNSRequestLimit = 3 * time.Second
)

func removeOpenDNSRequest(pid int, fqdn string) {
	openDNSRequestsLock.Lock()
	defer openDNSRequestsLock.Unlock()

	key := strconv.Itoa(pid) + "/" + fqdn
	delete(openDNSRequests, key)
}

func saveOpenDNSRequest(conn *Connection) {
	openDNSRequestsLock.Lock()
	defer openDNSRequestsLock.Unlock()

	key := strconv.Itoa(conn.process.Pid) + "/" + conn.Scope

	existingConn, ok := openDNSRequests[key]
	if ok {
		existingConn.Lock()
		defer existingConn.Unlock()

		existingConn.Ended = conn.Started
	} else {
		openDNSRequests[key] = conn
	}
}

func openDNSRequestWriter(ctx context.Context) error {
	ticker := time.NewTicker(writeOpenDNSRequestsTickDuration)

	for {
		select {
		case <-ctx.Done():
			ticker.Stop()
			return nil
		case <-ticker.C:
			writeOpenDNSRequestsToDB()
		}
	}
}

func writeOpenDNSRequestsToDB() {
	openDNSRequestsLock.Lock()
	defer openDNSRequestsLock.Unlock()

	threshold := time.Now().Add(-openDNSRequestLimit).Unix()
	for id, conn := range openDNSRequests {
		conn.Lock()
		if conn.Ended < threshold {
			conn.save()
			delete(openDNSRequests, id)
		}
		conn.Unlock()
	}
}
