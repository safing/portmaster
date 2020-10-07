package nameserver

import (
	"context"
	"errors"
	"net"
	"strings"

	"github.com/safing/portmaster/network/packet"

	"github.com/safing/portbase/modules/subsystems"

	"github.com/safing/portbase/log"
	"github.com/safing/portbase/modules"
	"github.com/safing/portmaster/firewall"
	"github.com/safing/portmaster/nameserver/nsutil"
	"github.com/safing/portmaster/netenv"
	"github.com/safing/portmaster/network"
	"github.com/safing/portmaster/network/netutils"
	"github.com/safing/portmaster/resolver"

	"github.com/miekg/dns"
)

var (
	module    *modules.Module
	dnsServer *dns.Server

	listenAddress = "0.0.0.0:53"
)

func init() {
	module = modules.Register("nameserver", nil, start, stop, "core", "resolver")
	subsystems.Register(
		"dns",
		"Secure DNS",
		"DNS resolver with scoping and DNS-over-TLS",
		module,
		"config:dns/",
		nil,
	)
}

func start() error {
	dnsServer = &dns.Server{Addr: listenAddress, Net: "udp"}
	dns.HandleFunc(".", handleRequestAsWorker)

	module.StartServiceWorker("dns resolver", 0, func(ctx context.Context) error {
		err := dnsServer.ListenAndServe()
		if err != nil {
			// check if we are shutting down
			if module.IsStopping() {
				return nil
			}
			// is something blocking our port?
			checkErr := checkForConflictingService()
			if checkErr != nil {
				return checkErr
			}
		}
		return err
	})

	return nil
}

func stop() error {
	if dnsServer != nil {
		return dnsServer.Shutdown()
	}
	return nil
}

func handleRequestAsWorker(w dns.ResponseWriter, query *dns.Msg) {
	err := module.RunWorker("dns request", func(ctx context.Context) error {
		return handleRequest(ctx, w, query)
	})
	if err != nil {
		log.Warningf("intel: failed to handle dns request: %s", err)
	}
}

func handleRequest(ctx context.Context, w dns.ResponseWriter, request *dns.Msg) error { //nolint:gocognit // TODO
	// Only process first question, that's how everyone does it.
	question := request.Question[0]
	q := &resolver.Query{
		FQDN:  question.Name,
		QType: dns.Type(question.Qtype),
	}

	// Get remote address of request.
	remoteAddr, ok := w.RemoteAddr().(*net.UDPAddr)
	if !ok {
		log.Warningf("nameserver: failed to get remote address of request for %s%s, ignoring", q.FQDN, q.QType)
		return nil
	}

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
		return sendResponse(ctx, w, request, responder, rrProviders...)
	}

	// Return with server failure if offline.
	if netenv.GetOnlineStatus() == netenv.StatusOffline &&
		!netenv.IsConnectivityDomain(q.FQDN) {
		tracer.Debugf("nameserver: not resolving %s, device is offline", q.FQDN)
		return reply(nsutil.ServerFailure("resolving disabled, device is offline"))
	}

	// Check the Query Class.
	if question.Qclass != dns.ClassINET {
		// we only serve IN records, return nxdomain
		tracer.Warningf("nameserver: only IN record requests are supported but received Qclass %d, returning NXDOMAIN", question.Qclass)
		return reply(nsutil.Refused("unsupported qclass"))
	}

	// Handle request for localhost.
	if strings.HasSuffix(q.FQDN, "localhost.") {
		tracer.Tracef("nameserver: returning localhost records")
		return reply(nsutil.Localhost())
	}

	// Authenticate request - only requests from the local host, but with any of its IPs, are allowed.
	local, err := netenv.IsMyIP(remoteAddr.IP)
	if err != nil {
		tracer.Warningf("nameserver: failed to check if request for %s%s is local: %s", q.FQDN, q.QType, err)
		return nil // Do no reply, drop request immediately.
	}
	if !local {
		tracer.Warningf("nameserver: external request for %s%s, ignoring", q.FQDN, q.QType)
		return nil // Do no reply, drop request immediately.
	}

	// Validate domain name.
	if !netutils.IsValidFqdn(q.FQDN) {
		tracer.Debugf("nameserver: domain name %s is invalid, refusing", q.FQDN)
		return reply(nsutil.Refused("invalid domain"))
	}

	// Get connection for this request. This identifies the process behind the request.
	conn := network.NewConnectionFromDNSRequest(ctx, q.FQDN, nil, packet.IPv4, remoteAddr.IP, uint16(remoteAddr.Port))
	conn.Lock()
	defer conn.Unlock()

	// Once we decided on the connection we might need to save it to the database,
	// so we defer that check for now.
	defer func() {
		switch conn.Verdict {
		// We immediately save blocked, dropped or failed verdicts so
		// they pop up in the UI.
		case network.VerdictBlock, network.VerdictDrop, network.VerdictFailed:
			conn.Save()

		// For undecided or accepted connections we don't save them yet, because
		// that will happen later anyway.
		case network.VerdictUndecided, network.VerdictAccept,
			network.VerdictRerouteToNameserver, network.VerdictRerouteToTunnel:
			return

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
	if responder, ok := conn.ReasonContext.(nsutil.Responder); ok {
		// Save the request as open, as we don't know if there will be a connection or not.
		network.SaveOpenDNSRequest(conn)

		tracer.Infof("nameserver: handing over request for %s to special filter responder: %s", q.ID(), conn.Reason)
		return reply(responder)
	}

	// Check if there is Verdict to act upon.
	switch conn.Verdict {
	case network.VerdictBlock, network.VerdictDrop, network.VerdictFailed:
		tracer.Infof("nameserver: request for %s from %s %s", q.ID(), conn.Process(), conn.Verdict.Verb())
		return reply(conn, conn)
	}

	// Save security level to query, so that the resolver can react to configuration.
	q.SecurityLevel = conn.Process().Profile().SecurityLevel()

	// Resolve request.
	rrCache, err := resolver.Resolve(ctx, q)
	if err != nil {
		// React to special errors.
		switch {
		case errors.Is(err, resolver.ErrNotFound):
			tracer.Tracef("nameserver: %s", err)
			return reply(nsutil.NxDomain("nxdomain: " + err.Error()))
		case errors.Is(err, resolver.ErrBlocked):
			tracer.Tracef("nameserver: %s", err)
			return reply(nsutil.ZeroIP("blocked: " + err.Error()))
		case errors.Is(err, resolver.ErrLocalhost):
			tracer.Tracef("nameserver: returning localhost records")
			return reply(nsutil.Localhost())
		default:
			tracer.Warningf("nameserver: failed to resolve %s: %s", q.ID(), err)
			return reply(nsutil.ServerFailure("internal error: " + err.Error()))
		}
	}
	if rrCache == nil {
		tracer.Warning("nameserver: received successful, but empty reply from resolver")
		return reply(nsutil.ServerFailure("internal error: empty reply"))
	}

	tracer.Trace("nameserver: deciding on resolved dns")
	rrCache = firewall.DecideOnResolvedDNS(ctx, conn, q, rrCache)
	if rrCache == nil {
		// Check again if there is a responder from the firewall.
		if responder, ok := conn.ReasonContext.(nsutil.Responder); ok {
			// Save the request as open, as we don't know if there will be a connection or not.
			network.SaveOpenDNSRequest(conn)

			tracer.Infof("nameserver: handing over request for %s to filter responder: %s", q.ID(), conn.Reason)
			return reply(responder)
		}

		// Request was blocked by the firewall.
		switch conn.Verdict {
		case network.VerdictBlock, network.VerdictDrop, network.VerdictFailed:
			tracer.Infof("nameserver: %s request for %s from %s", conn.Verdict.Verb(), q.ID(), conn.Process())
			return reply(conn, conn)
		}
	}

	// Save dns request as open.
	defer network.SaveOpenDNSRequest(conn)

	// Reply with successful response.
	tracer.Infof("nameserver: returning %s response for %s to %s", conn.Verdict.Verb(), q.ID(), conn.Process())
	return reply(rrCache, conn, rrCache)
}
