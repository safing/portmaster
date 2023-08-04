package network

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/miekg/dns"
	"golang.org/x/exp/slices"

	"github.com/safing/portbase/log"
	"github.com/safing/portmaster/nameserver/nsutil"
	"github.com/safing/portmaster/process"
	"github.com/safing/portmaster/resolver"
)

var (
	openDNSRequests     = make(map[string]*Connection) // key: <pid>/fqdn
	openDNSRequestsLock sync.Mutex

	supportedDomainToIPRecordTypes = []uint16{
		dns.TypeA,
		dns.TypeAAAA,
		dns.TypeSVCB,
		dns.TypeHTTPS,
	}
)

const (
	// writeOpenDNSRequestsTickDuration defines the interval in which open dns
	// requests are written.
	writeOpenDNSRequestsTickDuration = 5 * time.Second

	// openDNSRequestLimit defines the duration after which DNS requests without
	// a following connection are logged.
	openDNSRequestLimit = 3 * time.Second
)

// IsSupportDNSRecordType returns whether the given DSN RR type is supported
// by the network package, as in the requests are specially handled and can be
// "merged" into the resulting connection.
func IsSupportDNSRecordType(rrType uint16) bool {
	return slices.Contains[[]uint16, uint16](supportedDomainToIPRecordTypes, rrType)
}

func getDNSRequestCacheKey(pid int, fqdn string, qType uint16) string {
	return strconv.Itoa(pid) + "/" + fqdn + dns.Type(qType).String()
}

func removeOpenDNSRequest(pid int, fqdn string) {
	openDNSRequestsLock.Lock()
	defer openDNSRequestsLock.Unlock()

	// Delete PID-specific requests.
	for _, dnsType := range supportedDomainToIPRecordTypes {
		delete(openDNSRequests, getDNSRequestCacheKey(pid, fqdn, dnsType))
	}

	// If process is known, also check for non-attributed requests.
	if pid != process.UnidentifiedProcessID {
		for _, dnsType := range supportedDomainToIPRecordTypes {
			delete(openDNSRequests, getDNSRequestCacheKey(process.UnidentifiedProcessID, fqdn, dnsType))
		}
	}
}

// SaveOpenDNSRequest saves a dns request connection that was allowed to proceed.
func SaveOpenDNSRequest(q *resolver.Query, rrCache *resolver.RRCache, conn *Connection) {
	// Only save requests that actually went out (or triggered an async resolve) to reduce clutter.
	if rrCache == nil || (rrCache.ServedFromCache && !rrCache.RequestingNew) {
		return
	}

	// Try to "merge" supported requests into the resulting connection.
	// Save others immediately.
	if !IsSupportDNSRecordType(uint16(q.QType)) {
		conn.Save()
		return
	}

	openDNSRequestsLock.Lock()
	defer openDNSRequestsLock.Unlock()

	// Do not check for an existing open DNS request, as duplicates in such quick
	// succession are not worth keeping.
	// DNS queries are usually retried pretty quick.

	// Save to open dns requests.
	key := getDNSRequestCacheKey(conn.process.Pid, conn.Entity.Domain, uint16(q.QType))
	openDNSRequests[key] = conn
}

func openDNSRequestWriter(ctx context.Context) error {
	ticker := module.NewSleepyTicker(writeOpenDNSRequestsTickDuration, 0)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.Wait():
			writeOpenDNSRequestsToDB()
		}
	}
}

func writeOpenDNSRequestsToDB() {
	openDNSRequestsLock.Lock()
	defer openDNSRequestsLock.Unlock()

	threshold := time.Now().Add(-openDNSRequestLimit).Unix()
	for id, conn := range openDNSRequests {
		func() {
			conn.Lock()
			defer conn.Unlock()

			if conn.Ended < threshold {
				conn.Save()
				delete(openDNSRequests, id)
			}
		}()
	}
}

// ReplyWithDNS creates a new reply to the given request with the data from the RRCache, and additional informational records.
func (conn *Connection) ReplyWithDNS(ctx context.Context, request *dns.Msg) *dns.Msg {
	// Select request responder.
	switch conn.Verdict.Active {
	case VerdictBlock:
		return nsutil.BlockIP().ReplyWithDNS(ctx, request)
	case VerdictDrop:
		return nil // Do not respond to request.
	case VerdictFailed:
		return nsutil.BlockIP().ReplyWithDNS(ctx, request)
	case VerdictUndecided, VerdictUndeterminable,
		VerdictAccept, VerdictRerouteToNameserver, VerdictRerouteToTunnel:
		fallthrough
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
	switch conn.Verdict.Active {
	case VerdictFailed:
		level = log.ErrorLevel
	case VerdictUndecided, VerdictUndeterminable,
		VerdictAccept, VerdictBlock, VerdictDrop,
		VerdictRerouteToNameserver, VerdictRerouteToTunnel:
		fallthrough
	default:
		level = log.InfoLevel
	}

	// Create resource record with verdict and reason.
	rr, err := nsutil.MakeMessageRecord(level, fmt.Sprintf("%s: %s", conn.VerdictVerb(), conn.Reason.Msg))
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
