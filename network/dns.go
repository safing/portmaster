package network

import (
	"context"
	"strconv"
	"sync"
	"time"

	"github.com/safing/portmaster/process"
)

var (
	openDNSRequests     = make(map[string]*Connection) // key: <pid>/fqdn
	openDNSRequestsLock sync.Mutex

	// write open dns requests every
	writeOpenDNSRequestsTickDuration = 5 * time.Second

	// duration after which DNS requests without a following connection are logged
	openDNSRequestLimit = 3 * time.Second

	// scope prefix
	unidentifiedProcessScopePrefix = strconv.Itoa(process.UnidentifiedProcessID) + "/"
)

func removeOpenDNSRequest(pid int, fqdn string) {
	openDNSRequestsLock.Lock()
	defer openDNSRequestsLock.Unlock()

	key := strconv.Itoa(pid) + "/" + fqdn
	_, ok := openDNSRequests[key]
	if ok {
		delete(openDNSRequests, key)
	} else if pid != process.UnidentifiedProcessID {
		// check if there is an open dns request from an unidentified process
		delete(openDNSRequests, unidentifiedProcessScopePrefix+fqdn)
	}
}

// SaveOpenDNSRequest saves a dns request connection that was allowed to proceed.
func SaveOpenDNSRequest(conn *Connection) {
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
			conn.Save()
			delete(openDNSRequests, id)
		}
		conn.Unlock()
	}
}
