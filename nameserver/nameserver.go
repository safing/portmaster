package nameserver

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/safing/portbase/log"
	"github.com/safing/portmaster/firewall"
	"github.com/safing/portmaster/nameserver/nsutil"
	"github.com/safing/portmaster/netenv"
	"github.com/safing/portmaster/network"
	"github.com/safing/portmaster/network/netutils"
	"github.com/safing/portmaster/resolver"

	"github.com/miekg/dns"
)

func handleRequestAsWorker(w dns.ResponseWriter, query *dns.Msg) {
	err := module.RunWorker("dns request", func(ctx context.Context) error {
		return handleRequest(ctx, w, query)
	})
	if err != nil {
		log.Warningf("nameserver: failed to handle dns request: %s", err)
	}
}

func handleRequest(ctx context.Context, w dns.ResponseWriter, request *dns.Msg) error { //nolint:gocognit // TODO
	// Record metrics.
	startTime := time.Now()
	defer requestsHistogram.UpdateDuration(startTime)

	// Only process first question, that's how everyone does it.
	if len(request.Question) == 0 {
		return errors.New("missing question")
	}
	originalQuestion := request.Question[0]

	// Check if we are handling a non-standard query name.
	var nonStandardQuestionFormat bool
	lowerCaseQuestion := strings.ToLower(originalQuestion.Name)
	if lowerCaseQuestion != originalQuestion.Name {
		nonStandardQuestionFormat = true
	}

	// Create query for the resolver.
	q := &resolver.Query{
		FQDN:  lowerCaseQuestion,
		QType: dns.Type(originalQuestion.Qtype),
	}

	// Get remote address of request.
	remoteAddr, ok := w.RemoteAddr().(*net.UDPAddr)
	if !ok {
		log.Warningf("nameserver: failed to get remote address of request for %s%s, ignoring", q.FQDN, q.QType)
		return nil
	}
	// log.Errorf("DEBUG: nameserver: handling new request for %s from %s:%d", q.ID(), remoteAddr.IP, remoteAddr.Port)

	// Start context tracer for context-aware logging.
	ctx, tracer := log.AddTracer(ctx)
	defer tracer.Submit()
	tracer.Tracef("nameserver: handling new request for %s from %s:%d", q.ID(), remoteAddr.IP, remoteAddr.Port)

	// Check if there are more than one question.
	if len(request.Question) > 1 {
		tracer.Warningf("nameserver: received more than one question from (%s:%d), first question is %s", remoteAddr.IP, remoteAddr.Port, q.ID())
	}

	// Setup quick reply function.
	reply := func(responder nsutil.Responder, rrProviders ...nsutil.RRProvider) error {
		err := sendResponse(ctx, w, request, responder, rrProviders...)
		// Log error here instead of returning it in order to keep the context.
		if err != nil {
			tracer.Errorf("nameserver: %s", err)
		}
		return nil
	}

	// Check the Query Class.
	if originalQuestion.Qclass != dns.ClassINET {
		// we only serve IN records, return nxdomain
		tracer.Warningf("nameserver: only IN record requests are supported but received QClass %d, returning NXDOMAIN", originalQuestion.Qclass)
		return reply(nsutil.Refused("unsupported qclass"))
	}

	// Handle request for localhost.
	if strings.HasSuffix(q.FQDN, "localhost.") {
		tracer.Tracef("nameserver: returning localhost records")
		return reply(nsutil.Localhost())
	}

	// Validate domain name.
	if !netutils.IsValidFqdn(q.FQDN) {
		tracer.Debugf("nameserver: domain name %s is invalid, refusing", q.FQDN)
		return reply(nsutil.Refused("invalid domain"))
	}

	// Authenticate request - only requests from the local host, but with any of its IPs, are allowed.
	local, err := netenv.IsMyIP(remoteAddr.IP)
	if err != nil {
		tracer.Warningf("nameserver: failed to check if request for %s is local: %s", q.ID(), err)
		return nil // Do no reply, drop request immediately.
	}

	// Create connection ID for dns request.
	connID := fmt.Sprintf(
		"%s-%d-#%d-%s",
		remoteAddr.IP,
		remoteAddr.Port,
		request.Id,
		q.ID(),
	)

	// Get connection for this request. This identifies the process behind the request.
	var conn *network.Connection
	switch {
	case local:
		conn = network.NewConnectionFromDNSRequest(ctx, q.FQDN, nil, connID, remoteAddr.IP, uint16(remoteAddr.Port))

	case networkServiceMode():
		conn, err = network.NewConnectionFromExternalDNSRequest(ctx, q.FQDN, nil, connID, remoteAddr.IP)
		if err != nil {
			tracer.Warningf("nameserver: failed to get host/profile for request for %s%s: %s", q.FQDN, q.QType, err)
			return nil // Do no reply, drop request immediately.
		}

	default:
		tracer.Warningf("nameserver: external request for %s%s, ignoring", q.FQDN, q.QType)
		return nil // Do no reply, drop request immediately.
	}
	conn.Lock()
	defer conn.Unlock()

	// Create reference for the rrCache.
	var rrCache *resolver.RRCache

	// Once we decided on the connection we might need to save it to the database,
	// so we defer that check for now.
	defer func() {
		switch conn.Verdict {
		// We immediately save blocked, dropped or failed verdicts so
		// they pop up in the UI.
		case network.VerdictBlock, network.VerdictDrop, network.VerdictFailed, network.VerdictRerouteToNameserver, network.VerdictRerouteToTunnel:
			conn.Save()

		// For undecided or accepted connections we don't save them yet, because
		// that will happen later anyway.
		case network.VerdictUndecided, network.VerdictAccept:
			// Save the request as open, as we don't know if there will be a connection or not.
			network.SaveOpenDNSRequest(conn, uint16(q.QType))
			firewall.UpdateIPsAndCNAMEs(q, rrCache, conn)

		default:
			tracer.Warningf("nameserver: unexpected verdict %s for connection %s, not saving", conn.Verdict, conn)
		}
	}()

	// Check request with the privacy filter before resolving.
	firewall.DecideOnConnection(ctx, conn, nil)

	// Check if there is a responder from the firewall.
	// In special cases, the firewall might want to respond the query itself.
	// A reason for this might be that the request is sink-holed to a forced
	// IP address in which case we "accept" it, but let the firewall handle
	// the resolving as it wishes.
	if responder, ok := conn.Reason.Context.(nsutil.Responder); ok {
		tracer.Infof("nameserver: handing over request for %s to special filter responder: %s", q.ID(), conn.Reason.Msg)
		return reply(responder)
	}

	// Check if there is a Verdict to act upon.
	switch conn.Verdict {
	case network.VerdictBlock, network.VerdictDrop, network.VerdictFailed:
		tracer.Infof(
			"nameserver: returning %s response for %s to %s",
			conn.Verdict.Verb(),
			q.ID(),
			conn.Process(),
		)
		return reply(conn, conn)
	}

	// Save security level to query, so that the resolver can react to configuration.
	q.SecurityLevel = conn.Process().Profile().SecurityLevel()

	// Resolve request.
	rrCache, err = resolver.Resolve(ctx, q)
	// Handle error.
	if err != nil {
		switch {
		case errors.Is(err, resolver.ErrNotFound):
			tracer.Tracef("nameserver: %s", err)
			return reply(nsutil.NxDomain("nxdomain: " + err.Error()))
		case errors.Is(err, resolver.ErrBlocked):
			tracer.Tracef("nameserver: %s", err)
			return reply(nsutil.BlockIP("blocked: " + err.Error()))
		case errors.Is(err, resolver.ErrLocalhost):
			tracer.Tracef("nameserver: returning localhost records")
			return reply(nsutil.Localhost())
		case errors.Is(err, resolver.ErrOffline):
			if rrCache == nil {
				log.Tracer(ctx).Debugf("nameserver: not resolving %s, device is offline", q.ID())
				return reply(nsutil.ServerFailure(err.Error()))
			}
			// If an rrCache was returned, it's usable a backup.
			rrCache.IsBackup = true
			log.Tracer(ctx).Debugf("nameserver: device is offline, using backup cache for %s", q.ID())
		default:
			tracer.Warningf("nameserver: failed to resolve %s: %s", q.ID(), err)
			return reply(nsutil.ServerFailure("internal error: " + err.Error()))
		}
	}
	// Handle special cases.
	switch {
	case rrCache == nil:
		tracer.Warning("nameserver: received successful, but empty reply from resolver")
		return reply(nsutil.ServerFailure("internal error: empty reply"))
	case rrCache.RCode == dns.RcodeNameError:
		return reply(nsutil.NxDomain("no answer found (NXDomain)"))
	}

	tracer.Trace("nameserver: deciding on resolved dns")
	rrCache = firewall.FilterResolvedDNS(ctx, conn, q, rrCache)

	// Check again if there is a responder from the firewall.
	if responder, ok := conn.Reason.Context.(nsutil.Responder); ok {
		tracer.Infof("nameserver: handing over request for %s to special filter responder: %s", q.ID(), conn.Reason.Msg)
		return reply(responder)
	}

	// Check if there is a Verdict to act upon.
	switch conn.Verdict {
	case network.VerdictBlock, network.VerdictDrop, network.VerdictFailed:
		tracer.Infof(
			"nameserver: returning %s response for %s to %s",
			conn.Verdict.Verb(),
			q.ID(),
			conn.Process(),
		)
		return reply(conn, conn, rrCache)
	}

	// Revert back to non-standard question format, if we had to convert.
	if nonStandardQuestionFormat {
		rrCache.ReplaceAnswerNames(originalQuestion.Name)
	}

	// Reply with successful response.
	noAnswerIndicator := ""
	if len(rrCache.Answer) == 0 {
		noAnswerIndicator = "/no answer"
	}
	tracer.Infof(
		"nameserver: returning %s response (%s%s) for %s to %s",
		conn.Verdict.Verb(),
		dns.RcodeToString[rrCache.RCode],
		noAnswerIndicator,
		q.ID(),
		conn.Process(),
	)
	return reply(rrCache, conn, rrCache)
}
