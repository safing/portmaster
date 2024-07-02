package nameserver

import (
	"context"
	"fmt"

	"github.com/miekg/dns"

	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/service/nameserver/nsutil"
)

// sendResponse sends a response to query using w. The response message is
// created by responder. If addExtraRRs is not nil and implements the
// RRProvider interface then it will be also used to add more RRs in the
// extra section.
func sendResponse(
	ctx context.Context,
	w dns.ResponseWriter,
	request *dns.Msg,
	responder nsutil.Responder,
	rrProviders ...nsutil.RRProvider,
) error {
	// Have the Responder craft a DNS reply.
	reply := responder.ReplyWithDNS(ctx, request)
	if reply == nil {
		// Dropping query.
		return nil
	}
	// Signify that we are a recursive resolver.
	// While we do not handle recursion directly, we can safely assume, that we
	// always forward to a recursive resolver.
	reply.RecursionAvailable = true

	// Add extra RRs through a custom RRProvider.
	for _, rrProvider := range rrProviders {
		if rrProvider != nil {
			rrs := rrProvider.GetExtraRRs(ctx, request)
			reply.Extra = append(reply.Extra, rrs...)
		}
	}

	// Write reply.
	if err := writeDNSResponse(ctx, w, reply); err != nil {
		return fmt.Errorf("failed to send response: %w", err)
	}

	return nil
}

func writeDNSResponse(ctx context.Context, w dns.ResponseWriter, m *dns.Msg) (err error) {
	defer func() {
		// recover from panic
		if panicErr := recover(); panicErr != nil {
			err = fmt.Errorf("panic: %s", panicErr)
			log.Tracer(ctx).Debugf("nameserver: panic caused by this msg: %#v", m)
		}
	}()

	err = w.WriteMsg(m)
	if err != nil {
		// If we receive an error we might have exceeded the message size with all
		// our extra information records. Retry again without the extra section.
		log.Tracer(ctx).Tracef("nameserver: retrying to write dns message without extra section, error was: %s", err)
		m.Extra = nil
		noExtraErr := w.WriteMsg(m)
		if noExtraErr == nil {
			return fmt.Errorf("failed to write dns message without extra section: %w", err)
		}
	}
	return
}
