package nameserver

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/miekg/dns"

	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/service/firewall"
	"github.com/safing/portmaster/service/mgr"
	"github.com/safing/portmaster/service/nameserver/nsutil"
	"github.com/safing/portmaster/service/netenv"
	"github.com/safing/portmaster/service/network"
	"github.com/safing/portmaster/service/network/netutils"
	"github.com/safing/portmaster/service/resolver"
)

var hostname string

const internalError = "internal error: "

func handleRequestAsWorker(w dns.ResponseWriter, query *dns.Msg) {
	err := module.mgr.Do("handle dns request", func(wc *mgr.WorkerCtx) error {
		return handleRequest(wc.Ctx(), w, query)
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

	// Count request.
	totalHandledRequests.Inc()

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

	// Get public suffix after validation.
	q.InitPublicSuffixData()

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
				internalError+failingErr.Error(),
				"delayed failing query to mitigate request flooding",
			))
		}
		// Delay for default duration.
		tracer.Tracef("nameserver: delaying failing lookup for %s", failingDelay.Round(time.Millisecond))
		time.Sleep(failingDelay)
		return reply(nsutil.ServerFailure(
			internalError+failingErr.Error(),
			"delayed failing query to mitigate request flooding",
			fmt.Sprintf("error is cached for another %s", remainingFailingDuration.Round(time.Millisecond)),
		))
	}

	// Check if the request is local.
	local, err := netenv.IsMyIP(remoteAddr.IP)
	if err != nil {
		tracer.Warningf("nameserver: failed to check if request for %s is local: %s", q.ID(), err)
		return reply(nsutil.ServerFailure(internalError + " failed to check if request is local"))
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
			return reply(nsutil.ServerFailure(internalError + "failed to get profile"))
		}

	default:
		tracer.Warningf("nameserver: external request from %s for %s%s, ignoring", remoteAddr, q.FQDN, q.QType)
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
			conn.Entity.IPScope = rrCache.Resolver.IPScope
		} else {
			// Get resolvers for this query to determine the resolving scope.
			resolvers, _, _ := resolver.GetResolversInScope(ctx, q)
			if len(resolvers) > 0 {
				conn.Entity.IPScope = resolvers[0].Info.IPScope
			}
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
				conn.Failed(internalError+"no reply", "")
				return
			}

			// Mark successful queries as internal in order to hide them in the simple interface.
			// These requests were most probably made for another process and only add confusion if listed.
			if conn.Process().IsSystemResolver() {
				conn.Internal = true
			}

			// Save the request as open, as we don't know if there will be a connection or not.
			firewall.UpdateIPsAndCNAMEs(q, rrCache, conn)
			network.SaveOpenDNSRequest(q, rrCache, conn)

		case network.VerdictUndeterminable:
			fallthrough
		default:
			tracer.Warningf("nameserver: unexpected verdict %s for connection %s, not saving", conn.VerdictVerb(), conn)
		}
	}()

	// Check request with the privacy filter before resolving.
	firewall.FilterConnection(ctx, conn, nil, true, false)

	// Check if there is a responder from the firewall.
	// In special cases, the firewall might want to respond the query itself.
	// A reason for this might be that the request is sink-holed to a forced
	// IP address in which case we "accept" it, but let the firewall handle
	// the resolving as it wishes.
	if responder, ok := conn.Reason.Context.(nsutil.Responder); ok {
		tracer.Infof("nameserver: handing over request for %s to special filter responder: %s", q.ID(), conn.Reason.Msg)
		return reply(responder, conn)
	}

	// Check if there is a Verdict to act upon.
	switch conn.Verdict { //nolint:exhaustive // Only checking for specific values.
	case network.VerdictBlock, network.VerdictDrop, network.VerdictFailed:
		tracer.Infof(
			"nameserver: returning %s response for %s to %s",
			conn.VerdictVerb(),
			q.ID(),
			conn.Process(),
		)
		return reply(conn, conn)
	}

	// Resolve request.
	rrCache, err = resolver.Resolve(ctx, q)
	// Handle error.
	if err != nil {
		switch {
		case errors.Is(err, resolver.ErrNotFound):
			// Try alternatives domain names for unofficial domain spaces.
			rrCache = checkAlternativeCaches(ctx, q)
			if rrCache == nil {
				tracer.Tracef("nameserver: %s", err)
				conn.Failed("domain does not exist", "")
				return reply(nsutil.NxDomain("nxdomain: " + err.Error()))
			}
		case errors.Is(err, resolver.ErrBlocked):
			tracer.Tracef("nameserver: %s", err)
			conn.Block(err.Error(), "")
			return reply(nsutil.BlockIP("blocked: " + err.Error()))

		case errors.Is(err, resolver.ErrLocalhost):
			tracer.Tracef("nameserver: returning localhost records")
			conn.Accept("allowing query for localhost", "")
			return reply(nsutil.Localhost())

		case errors.Is(err, resolver.ErrOffline):
			if rrCache == nil {
				tracer.Debugf("nameserver: not resolving %s, device is offline", q.ID())
				conn.Failed("not resolving, device is offline", "")
				return reply(nsutil.ServerFailure(err.Error()))
			}
			// If an rrCache was returned, it's usable as a backup.
			rrCache.IsBackup = true
			log.Tracer(ctx).Debugf("nameserver: device is offline, using backup cache for %s", q.ID())

		default:
			tracer.Warningf("nameserver: failed to resolve %s: %s", q.ID(), err)
			conn.Failed(fmt.Sprintf("query failed: %s", err), "")
			addFailingQuery(q, err)
			return reply(nsutil.ServerFailure(internalError + err.Error()))
		}
	}
	// Handle special cases.
	switch {
	case rrCache == nil:
		tracer.Warning("nameserver: received successful, but empty reply from resolver")
		addFailingQuery(q, errors.New("emptry reply from resolver"))
		return reply(nsutil.ServerFailure(internalError + "empty reply"))
	case rrCache.RCode == dns.RcodeNameError:
		// Try alternatives domain names for unofficial domain spaces.
		altRRCache := checkAlternativeCaches(ctx, q)
		if altRRCache != nil {
			rrCache = altRRCache
		} else {
			// Return now if NXDomain.
			return reply(nsutil.NxDomain("no answer found (NXDomain)"))
		}

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
			conn.VerdictVerb(),
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
		conn.VerdictVerb(),
		dns.RcodeToString[rrCache.RCode],
		noAnswerIndicator,
		q.ID(),
		conn.Process(),
	)
	return reply(rrCache, conn, rrCache)
}

func checkAlternativeCaches(ctx context.Context, q *resolver.Query) *resolver.RRCache {
	// Do not try alternatives when the query is in a public suffix.
	// This also includes arpa. and local.
	if q.ICANNSpace {
		return nil
	}

	// Check if the env resolver has something.
	pmEnvQ := &resolver.Query{
		FQDN:  q.FQDN + "local." + resolver.InternalSpecialUseDomain,
		QType: q.QType,
	}
	rrCache, err := resolver.QueryPortmasterEnv(ctx, pmEnvQ)
	if err == nil && rrCache != nil && rrCache.RCode == dns.RcodeSuccess {
		makeAlternativeRecord(ctx, q, rrCache, pmEnvQ.FQDN)
		return rrCache
	}

	// Check if we have anything in cache
	localFQDN := q.FQDN + "local."
	rrCache, err = resolver.GetRRCache(localFQDN, q.QType)
	if err == nil && rrCache != nil && rrCache.RCode == dns.RcodeSuccess {
		makeAlternativeRecord(ctx, q, rrCache, localFQDN)
		return rrCache
	}

	return nil
}

func makeAlternativeRecord(ctx context.Context, q *resolver.Query, rrCache *resolver.RRCache, altName string) {
	log.Tracer(ctx).Debugf("using %s to answer query", altName)

	// Duplicate answers so they match the query.
	copied := make([]dns.RR, 0, len(rrCache.Answer))
	for _, answer := range rrCache.Answer {
		if strings.ToLower(answer.Header().Name) == altName {
			copiedAnswer := dns.Copy(answer)
			copiedAnswer.Header().Name = q.FQDN
			copied = append(copied, copiedAnswer)
		}
	}
	if len(copied) > 0 {
		rrCache.Answer = append(rrCache.Answer, copied...)
	}

	// Update the question.
	rrCache.Domain = q.FQDN
}
