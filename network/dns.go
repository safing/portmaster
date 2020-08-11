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

func getDNSRequestCacheKey(pid int, fqdn string) string {
	return strconv.Itoa(pid) + "/" + fqdn
}

func removeOpenDNSRequest(pid int, fqdn string) {
	openDNSRequestsLock.Lock()
	defer openDNSRequestsLock.Unlock()

	key := getDNSRequestCacheKey(pid, fqdn)
	_, ok := openDNSRequests[key]
	if ok {
		delete(openDNSRequests, key)
		return
	}

	if pid != process.UnidentifiedProcessID {
		// check if there is an open dns request from an unidentified process
		delete(openDNSRequests, unidentifiedProcessScopePrefix+fqdn)
	}
}

// SaveOpenDNSRequest saves a dns request connection that was allowed to proceed.
func SaveOpenDNSRequest(conn *Connection) {
	openDNSRequestsLock.Lock()
	defer openDNSRequestsLock.Unlock()

	key := getDNSRequestCacheKey(conn.process.Pid, conn.Scope)
	if existingConn, ok := openDNSRequests[key]; ok {
		existingConn.Lock()
		defer existingConn.Unlock()
		existingConn.Ended = conn.Started
		return
	}

	openDNSRequests[key] = conn
}

func openDNSRequestWriter(ctx context.Context) error {
	ticker := time.NewTicker(writeOpenDNSRequestsTickDuration)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
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
