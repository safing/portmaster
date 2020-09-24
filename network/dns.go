package network

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/miekg/dns"
	"github.com/safing/portbase/log"
	"github.com/safing/portmaster/nameserver/nsutil"
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

// ReplyWithDNS creates a new reply to the given request with the data from the RRCache, and additional informational records.
func (conn *Connection) ReplyWithDNS(ctx context.Context, request *dns.Msg) *dns.Msg {
	// Select request responder.
	switch conn.Verdict {
	case VerdictBlock:
		return nsutil.ZeroIP().ReplyWithDNS(ctx, request)
	case VerdictDrop:
		return nil // Do not respond to request.
	case VerdictFailed:
		return nsutil.ZeroIP().ReplyWithDNS(ctx, request)
	default:
		reply := nsutil.ServerFailure().ReplyWithDNS(ctx, request)
		nsutil.AddMessageToReply(ctx, reply, log.ErrorLevel, "INTERNAL ERROR: incorrect use of network.Connection's DNS Responder")
		return reply
	}
}

// GetExtraRRs returns a slice of RRs with additional informational records.
func (conn *Connection) GetExtraRRs(ctx context.Context, request *dns.Msg) []dns.RR {
	// Select level to add the verdict record with.
	var level log.Severity
	switch conn.Verdict {
	case VerdictFailed:
		level = log.ErrorLevel
	default:
		level = log.InfoLevel
	}

	// Create resource record with verdict and reason.
	rr, err := nsutil.MakeMessageRecord(level, fmt.Sprintf("%s: %s", conn.Verdict.Verb(), conn.Reason))
	if err != nil {
		log.Tracer(ctx).Warningf("filter: failed to add informational record to reply: %s", err)
		return nil
	}
	extra := []dns.RR{rr}

	// Add additional records from ReasonContext.
	if rrProvider, ok := conn.ReasonContext.(nsutil.RRProvider); ok {
		rrs := rrProvider.GetExtraRRs(ctx, request)
		extra = append(extra, rrs...)
	}

	return extra
}
