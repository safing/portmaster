package nsutil

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/miekg/dns"

	"github.com/safing/portmaster/base/log"
)

// ErrNilRR is returned when a parsed RR is nil.
var ErrNilRR = errors.New("is nil")

// Responder defines the interface that any block/deny reason interface
// may implement to support sending custom DNS responses for a given reason.
// That is, if a reason context implements the Responder interface the
// ReplyWithDNS method will be called instead of creating the default
// zero-ip response.
type Responder interface {
	// ReplyWithDNS is called when a DNS response to a DNS message is
	// crafted because the request is either denied or blocked.
	ReplyWithDNS(ctx context.Context, request *dns.Msg) *dns.Msg
}

// RRProvider defines the interface that any block/deny reason interface
// may implement to support adding additional DNS resource records to
// the DNS responses extra (additional) section.
type RRProvider interface {
	// GetExtraRRs is called when a DNS response to a DNS message is
	// crafted because the request is either denied or blocked.
	GetExtraRRs(ctx context.Context, request *dns.Msg) []dns.RR
}

// ResponderFunc is a convenience type to use a function
// directly as a Responder.
type ResponderFunc func(ctx context.Context, request *dns.Msg) *dns.Msg

// ReplyWithDNS implements the Responder interface and calls rf.
func (rf ResponderFunc) ReplyWithDNS(ctx context.Context, request *dns.Msg) *dns.Msg {
	return rf(ctx, request)
}

// MarshalJSON disables JSON marshaling for ResponderFunc.
func (rf ResponderFunc) MarshalJSON() ([]byte, error) {
	return json.Marshal(nil)
}

// BlockIP is a ResponderFunc than replies with either 0.0.0.17 or ::17 for
// each A or AAAA question respectively. If there is no A or AAAA question, it
// defaults to replying with NXDomain.
func BlockIP(msgs ...string) ResponderFunc {
	return createResponderFunc(
		"blocking",
		"0.0.0.17",
		"::17",
		msgs...,
	)
}

// ZeroIP is a ResponderFunc than replies with either 0.0.0.0 or :: for each A
// or AAAA question respectively. If there is no A or AAAA question, it
// defaults to replying with NXDomain.
func ZeroIP(msgs ...string) ResponderFunc {
	return createResponderFunc(
		"zero ip",
		"0.0.0.0",
		"::",
		msgs...,
	)
}

// Localhost is a ResponderFunc than replies with localhost IP addresses.
// If there is no A or AAAA question, it defaults to replying with NXDomain.
func Localhost(msgs ...string) ResponderFunc {
	return createResponderFunc(
		"localhost",
		"127.0.0.1",
		"::1",
		msgs...,
	)
}

func createResponderFunc(responderName, aAnswer, aaaaAnswer string, msgs ...string) ResponderFunc {
	return func(ctx context.Context, request *dns.Msg) *dns.Msg {
		reply := new(dns.Msg)
		hasErr := false

		for _, question := range request.Question {
			var rr dns.RR
			var err error

			switch question.Qtype {
			case dns.TypeA:
				rr, err = dns.NewRR(question.Name + " 1 IN A " + aAnswer)
			case dns.TypeAAAA:
				rr, err = dns.NewRR(question.Name + " 1 IN AAAA " + aaaaAnswer)
			}

			switch {
			case err != nil:
				log.Tracer(ctx).Errorf("nameserver: failed to create %s response for %s: %s", responderName, question.Name, err)
				hasErr = true
			case rr != nil:
				reply.Answer = append(reply.Answer, rr)
			}
		}

		switch {
		case hasErr && len(reply.Answer) == 0:
			reply.SetRcode(request, dns.RcodeServerFailure)
		case len(reply.Answer) == 0:
			reply.SetRcode(request, dns.RcodeNameError)
		default:
			reply.SetRcode(request, dns.RcodeSuccess)
		}

		AddMessagesToReply(ctx, reply, log.InfoLevel, msgs...)

		return reply
	}
}

// NxDomain returns a ResponderFunc that replies with NXDOMAIN.
func NxDomain(msgs ...string) ResponderFunc {
	return func(ctx context.Context, request *dns.Msg) *dns.Msg {
		reply := new(dns.Msg).SetRcode(request, dns.RcodeNameError)
		AddMessagesToReply(ctx, reply, log.InfoLevel, msgs...)

		// According to RFC4074 (https://tools.ietf.org/html/rfc4074), there are
		// nameservers that incorrectly respond with NXDomain instead of an empty
		// SUCCESS response when there are other RRs for the queried domain name.
		// This can lead to the software thinking that no RRs exist for that
		// domain. In order to mitigate this a bit, we slightly delay NXDomain
		// responses.
		time.Sleep(500 * time.Millisecond)

		return reply
	}
}

// Refused returns a ResponderFunc that replies with REFUSED.
func Refused(msgs ...string) ResponderFunc {
	return func(ctx context.Context, request *dns.Msg) *dns.Msg {
		reply := new(dns.Msg).SetRcode(request, dns.RcodeRefused)
		AddMessagesToReply(ctx, reply, log.InfoLevel, msgs...)
		return reply
	}
}

// ServerFailure returns a ResponderFunc that replies with SERVFAIL.
func ServerFailure(msgs ...string) ResponderFunc {
	return func(ctx context.Context, request *dns.Msg) *dns.Msg {
		reply := new(dns.Msg).SetRcode(request, dns.RcodeServerFailure)
		AddMessagesToReply(ctx, reply, log.InfoLevel, msgs...)
		return reply
	}
}

// MakeMessageRecord creates an informational resource record that can be added
// to the extra section of a reply.
func MakeMessageRecord(level log.Severity, msg string) (dns.RR, error) { //nolint:interfacer
	rr, err := dns.NewRR(fmt.Sprintf(
		`%s.portmaster. 0 IN TXT "%s"`,
		strings.ToLower(level.String()),
		msg,
	))
	if err != nil {
		return nil, err
	}
	if rr == nil {
		return nil, ErrNilRR
	}
	return rr, nil
}

// AddMessagesToReply creates information resource records using
// MakeMessageRecord and immediately adds them to the extra section of the given
// reply. If an error occurs, the resource record will not be added, and the
// error will be logged.
func AddMessagesToReply(ctx context.Context, reply *dns.Msg, level log.Severity, msgs ...string) {
	for _, msg := range msgs {
		// Ignore empty messages.
		if msg == "" {
			continue
		}

		// Create resources record.
		rr, err := MakeMessageRecord(level, msg)
		if err != nil {
			log.Tracer(ctx).Warningf("nameserver: failed to add message to reply: %s", err)
			continue
		}

		// Add to extra section of the reply.
		reply.Extra = append(reply.Extra, rr)
	}
}
