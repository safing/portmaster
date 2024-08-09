package network

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/miekg/dns"
	"golang.org/x/exp/slices"

	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/service/mgr"
	"github.com/safing/portmaster/service/nameserver/nsutil"
	"github.com/safing/portmaster/service/network/packet"
	"github.com/safing/portmaster/service/process"
	"github.com/safing/portmaster/service/resolver"
)

var (
	dnsRequestConnections     = make(map[string]*Connection) // key: <protocol>-<local ip>-<local port>
	dnsRequestConnectionsLock sync.RWMutex

	openDNSRequests     = make(map[string]*Connection) // key: <pid>/<fqdn>
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

func getDNSRequestConnectionKey(packetInfo *packet.Info) (id string, ok bool) {
	// We only support protocols with ports.
	if packetInfo.SrcPort == 0 {
		return "", false
	}

	return fmt.Sprintf("%d-%s-%d", packetInfo.Protocol, packetInfo.Src, packetInfo.SrcPort), true
}

// SaveDNSRequestConnection saves a dns request connection for later retrieval.
func SaveDNSRequestConnection(conn *Connection, pkt packet.Packet) {
	// Check connection.
	if conn.PID == process.UndefinedProcessID || conn.PID == process.SystemProcessID {
		// When re-injecting packets on Windows, they are reported with kernel PID (4).
		log.Tracer(pkt.Ctx()).Tracef("network: not saving dns request connection because the PID is undefined/kernel")
		return
	}

	// Create key.
	key, ok := getDNSRequestConnectionKey(pkt.Info())
	if !ok {
		log.Tracer(pkt.Ctx()).Debugf("network: not saving dns request connection %s because the protocol is not supported", pkt)
		return
	}

	// Add or update DNS request connection.
	log.Tracer(pkt.Ctx()).Tracef("network: saving %s with PID %d as dns request connection for fast DNS request attribution", pkt, conn.PID)
	dnsRequestConnectionsLock.Lock()
	defer dnsRequestConnectionsLock.Unlock()
	dnsRequestConnections[key] = conn
}

// GetDNSRequestConnection returns a saved dns request connection.
func GetDNSRequestConnection(packetInfo *packet.Info) (conn *Connection, ok bool) {
	// Make key.
	key, ok := getDNSRequestConnectionKey(packetInfo)
	if !ok {
		return nil, false
	}

	// Get and return
	dnsRequestConnectionsLock.RLock()
	defer dnsRequestConnectionsLock.RUnlock()

	conn, ok = dnsRequestConnections[key]
	return conn, ok
}

// deleteDNSRequestConnection removes a connection from the dns request connections.
func deleteDNSRequestConnection(packetInfo *packet.Info) { //nolint:unused,deadcode
	dnsRequestConnectionsLock.Lock()
	defer dnsRequestConnectionsLock.Unlock()

	key, ok := getDNSRequestConnectionKey(packetInfo)
	if ok {
		delete(dnsRequestConnections, key)
	}
}

// cleanDNSRequestConnections deletes old DNS request connections.
func cleanDNSRequestConnections() {
	deleteOlderThan := time.Now().Unix() - 3

	dnsRequestConnectionsLock.Lock()
	defer dnsRequestConnectionsLock.Unlock()

	for key, conn := range dnsRequestConnections {
		conn.Lock()

		if conn.Ended > 0 && conn.Ended < deleteOlderThan {
			delete(dnsRequestConnections, key)
		}

		conn.Unlock()
	}
}

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

func openDNSRequestWriter(ctx *mgr.WorkerCtx) error {
	module.dnsRequestTicker = mgr.NewSleepyTicker(writeOpenDNSRequestsTickDuration, 0)
	defer module.dnsRequestTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-module.dnsRequestTicker.Wait():
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
	switch conn.Verdict {
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
	switch conn.Verdict {
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
