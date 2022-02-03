package nameserver

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/miekg/dns"

	"github.com/safing/portbase/log"
	"github.com/safing/portmaster/firewall"
	"github.com/safing/portmaster/nameserver/nsutil"
	"github.com/safing/portmaster/netenv"
	"github.com/safing/portmaster/network"
	"github.com/safing/portmaster/network/netutils"
	"github.com/safing/portmaster/resolver"
)

var hostname string

func handleRequestAsWorker(w dns.ResponseWriter, query *dns.Msg) {
	err := module.RunWorker("dns request", func(ctx context.Context) error {
		return handleRequest(ctx, w, query)
	})
	if err != nil {
		log.Warningf("nameserver: failed to handle dns request: %s", err)
	}
}

func handleRequest(ctx context.Context, w dns.ResponseWriter, request *dns.Msg) error { //nolint:maintidx // TODO
	// Record metrics.
	startTime := time.Now()
	defer requestsHistogram.UpdateDuration(startTime)

	// Check Question, only process the first, that's how everyone does it.
	var originalQuestion dns.Question
	switch len(request.Question) {
	case 0:
		log.Warning("nameserver: received query without question")
		return sendResponse(ctx, w, request, nsutil.Refused("no question provided"))
	case 1:
		originalQuestion = request.Question[0]
	default:
		log.Warningf(
			"nameserver: received query with multiple questions, first is %s.%s",
			request.Question[0].Name,
			dns.Type(request.Question[0].Qtype),
		)
		return sendResponse(ctx, w, request, nsutil.Refused("multiple question provided"))
	}

	// Check the Query Class.
	if originalQuestion.Qclass != dns.ClassINET {
		// We only serve IN records.
		log.Warningf("nameserver: received unsupported qclass %d question for %s", originalQuestion.Qclass, originalQuestion.Name)
		return sendResponse(ctx, w, request, nsutil.Refused("unsupported qclass"))
	}

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
		log.Warningf("nameserver: failed to get remote address of dns query: is type %+T", w.RemoteAddr())
		return sendResponse(ctx, w, request, nsutil.Refused("unsupported transport"))
	}

	// Start context tracer for context-aware logging.
	ctx, tracer := log.AddTracer(ctx)
	defer tracer.Submit()
	tracer.Tracef("nameserver: handling new request for %s from %s:%d", q.ID(), remoteAddr.IP, remoteAddr.Port)

	// Setup quick reply function.
	reply := func(responder nsutil.Responder, rrProviders ...nsutil.RRProvider) error {
		err := sendResponse(ctx, w, request, responder, rrProviders...)
		// Log error here instead of returning it in order to keep the context.
		if err != nil {
			tracer.Errorf("nameserver: %s", err)
		}
		return nil
	}

	// Handle request for localhost and the hostname.
	if strings.HasSuffix(q.FQDN, "localhost.") || q.FQDN == hostname {
		tracer.Tracef("nameserver: returning localhost records")
		return reply(nsutil.Localhost())
	}

	// Validate domain name.
	if !netutils.IsValidFqdn(q.FQDN) {
		tracer.Debugf("nameserver: domain name %s is invalid, refusing", q.FQDN)
		return reply(nsutil.Refused("invalid domain"))
	}

	// Check if query is failing.
	// Some software retries failing queries excessively. This might not be a
	// problem normally, but handling a request is pretty expensive for the
	// Portmaster, as it has to find out who sent the query. If we know the query
	// will fail with a very high probability, it is beneficial to just kill the
	// query for some time before doing any expensive work.
	defer cleanFailingQueries(10, 3)
	failingUntil, failingErr := checkIfQueryIsFailing(q)
	if failingErr != nil {
		remainingFailingDuration := time.Until(*failingUntil)
		tracer.Debugf("nameserver: returning previous error for %s: %s", q.ID(), failingErr)

		// Delay the response a bit in order to mitigate request flooding.
		if remainingFailingDuration < failingDelay {
			// Delay for remainind fail duration.
			tracer.Tracef("nameserver: delaying failing lookup until end of fail duration for %s", remainingFailingDuration.Round(time.Millisecond))
			time.Sleep(remainingFailingDuration)
			return reply(nsutil.ServerFailure(
				"internal error: "+failingErr.Error(),
				"delayed failing query to mitigate request flooding",
			))
		}
		// Delay for default duration.
		tracer.Tracef("nameserver: delaying failing lookup for %s", failingDelay.Round(time.Millisecond))
		time.Sleep(failingDelay)
		return reply(nsutil.ServerFailure(
			"internal error: "+failingErr.Error(),
			"delayed failing query to mitigate request flooding",
			fmt.Sprintf("error is cached for another %s", remainingFailingDuration.Round(time.Millisecond)),
		))
	}

	// Check if the request is local.
	local, err := netenv.IsMyIP(remoteAddr.IP)
	if err != nil {
		tracer.Warningf("nameserver: failed to check if request for %s is local: %s", q.ID(), err)
		return reply(nsutil.ServerFailure("internal error: failed to check if request is local"))
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
			return reply(nsutil.ServerFailure("internal error: failed to get profile"))
		}

	default:
		tracer.Warningf("nameserver: external request for %s%s, ignoring", q.FQDN, q.QType)
		return reply(nsutil.Refused("external queries are not permitted"))
	}
	conn.Lock()
	defer conn.Unlock()

	// Create reference for the rrCache.
	var rrCache *resolver.RRCache

	// Once we decided on the connection we might need to save it to the database,
	// so we defer that check for now.
	defer func() {
		// Add metadata to connection.
		if rrCache != nil {
			conn.DNSContext = rrCache.ToDNSRequestContext()
			conn.Resolver = rrCache.Resolver
		}

		switch conn.Verdict {
		// We immediately save blocked, dropped or failed verdicts so
		// they pop up in the UI.
		case network.VerdictBlock, network.VerdictDrop, network.VerdictFailed, network.VerdictRerouteToNameserver, network.VerdictRerouteToTunnel:
			conn.Save()

		// For undecided or accepted connections we don't save them yet, because
		// that will happen later anyway.
		case network.VerdictUndecided, network.VerdictAccept:
			// Check if we have a response.
			if rrCache == nil {
				conn.Failed("internal error: no reply", "")
				return
			}

			// Save the request as open, as we don't know if there will be a connection or not.
			network.SaveOpenDNSRequest(q, rrCache, conn)
			firewall.UpdateIPsAndCNAMEs(q, rrCache, conn)

		case network.VerdictUndeterminable:
			fallthrough
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
	switch conn.Verdict { //nolint:exhaustive // Only checking for specific values.
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
		conn.Failed(fmt.Sprintf("query failed: %s", err), "")
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
			addFailingQuery(q, err)
			return reply(nsutil.ServerFailure("internal error: " + err.Error()))
		}
	}
	// Handle special cases.
	switch {
	case rrCache == nil:
		tracer.Warning("nameserver: received successful, but empty reply from resolver")
		addFailingQuery(q, errors.New("emptry reply from resolver"))
		return reply(nsutil.ServerFailure("internal error: empty reply"))
	case rrCache.RCode == dns.RcodeNameError:
		// Return now if NXDomain.
		return reply(nsutil.NxDomain("no answer found (NXDomain)"))
	}

	// Check with firewall again after resolving.
	tracer.Trace("nameserver: deciding on resolved dns")
	rrCache = firewall.FilterResolvedDNS(ctx, conn, q, rrCache)

	// Check again if there is a responder from the firewall.
	if responder, ok := conn.Reason.Context.(nsutil.Responder); ok {
		tracer.Infof("nameserver: handing over request for %s to special filter responder: %s", q.ID(), conn.Reason.Msg)
		return reply(responder)
	}

	// Check if there is a Verdict to act upon.
	switch conn.Verdict { //nolint:exhaustive // Only checking for specific values.
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
