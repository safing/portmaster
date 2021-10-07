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

	// scope prefix
	unidentifiedProcessScopePrefix = strconv.Itoa(process.UnidentifiedProcessID) + "/"
)

const (
	// write open dns requests every
	writeOpenDNSRequestsTickDuration = 5 * time.Second

	// duration after which DNS requests without a following connection are logged
	openDNSRequestLimit = 3 * time.Second
)

func getDNSRequestCacheKey(pid int, fqdn string, qType uint16) string {
	return strconv.Itoa(pid) + "/" + fqdn + dns.Type(qType).String()
}

func removeOpenDNSRequest(pid int, fqdn string) {
	openDNSRequestsLock.Lock()
	defer openDNSRequestsLock.Unlock()

	// Delete PID-specific requests.
	delete(openDNSRequests, getDNSRequestCacheKey(pid, fqdn, dns.TypeA))
	delete(openDNSRequests, getDNSRequestCacheKey(pid, fqdn, dns.TypeAAAA))

	// If process is known, also check for non-attributed requests.
	if pid != process.UnidentifiedProcessID {
		delete(openDNSRequests, getDNSRequestCacheKey(process.UnidentifiedProcessID, fqdn, dns.TypeA))
		delete(openDNSRequests, getDNSRequestCacheKey(process.UnidentifiedProcessID, fqdn, dns.TypeAAAA))
	}
}

// SaveOpenDNSRequest saves a dns request connection that was allowed to proceed.
func SaveOpenDNSRequest(conn *Connection, qType uint16) {
	openDNSRequestsLock.Lock()
	defer openDNSRequestsLock.Unlock()

	// Only save open A and AAAA requests.
	switch qType {
	case dns.TypeA, dns.TypeAAAA:
	default:
		return
	}

	key := getDNSRequestCacheKey(conn.process.Pid, conn.Entity.Domain, qType)
	if existingConn, ok := openDNSRequests[key]; ok {
		// End previous request and save it.
		existingConn.Lock()
		existingConn.Ended = conn.Started
		existingConn.Unlock()
		existingConn.Save()

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
		return nsutil.BlockIP().ReplyWithDNS(ctx, request)
	case VerdictDrop:
		return nil // Do not respond to request.
	case VerdictFailed:
		return nsutil.BlockIP().ReplyWithDNS(ctx, request)
	default:
		reply := nsutil.ServerFailure().ReplyWithDNS(ctx, request)
		nsutil.AddMessagesToReply(ctx, reply, log.ErrorLevel, "INTERNAL ERROR: incorrect use of Connection DNS Responder")
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
	rr, err := nsutil.MakeMessageRecord(level, fmt.Sprintf("%s: %s", conn.Verdict.Verb(), conn.Reason.Msg))
	if err != nil {
		log.Tracer(ctx).Warningf("filter: failed to add informational record to reply: %s", err)
		return nil
	}
	extra := []dns.RR{rr}

	// Add additional records from Reason.Context.
	if rrProvider, ok := conn.Reason.Context.(nsutil.RRProvider); ok {
		rrs := rrProvider.GetExtraRRs(ctx, request)
		extra = append(extra, rrs...)
	}

	return extra
}
