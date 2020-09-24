package nsutil

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/miekg/dns"
	"github.com/safing/portbase/log"
)

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

// ZeroIP is a ResponderFunc than replies with either 0.0.0.0 or :: for
// each A or AAAA question respectively.
func ZeroIP(msgs ...string) ResponderFunc {
	return func(ctx context.Context, request *dns.Msg) *dns.Msg {
		reply := new(dns.Msg)
		hasErr := false

		for _, question := range request.Question {
			var rr dns.RR
			var err error

			switch question.Qtype {
			case dns.TypeA:
				rr, err = dns.NewRR(question.Name + "  0	IN	A		0.0.0.0")
			case dns.TypeAAAA:
				rr, err = dns.NewRR(question.Name + "  0	IN	AAAA	::")
			}

			if err != nil {
				log.Tracer(ctx).Errorf("nameserver: failed to create zero-ip response for %s: %s", question.Name, err)
				hasErr = true
			} else {
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

		for _, msg := range msgs {
			AddMessageToReply(ctx, reply, log.InfoLevel, msg)
		}

		return reply
	}
}

// Localhost is a ResponderFunc than replies with localhost IP addresses.
func Localhost(msgs ...string) ResponderFunc {
	return func(ctx context.Context, request *dns.Msg) *dns.Msg {
		reply := new(dns.Msg)
		hasErr := false

		for _, question := range request.Question {
			var rr dns.RR
			var err error

			switch question.Qtype {
			case dns.TypeA:
				rr, err = dns.NewRR("localhost. 0 IN A 127.0.0.1")
			case dns.TypeAAAA:
				rr, err = dns.NewRR("localhost. 0 IN AAAA ::1")
			}

			if err != nil {
				log.Tracer(ctx).Errorf("nameserver: failed to create localhost response for %s: %s", question.Name, err)
				hasErr = true
			} else {
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

		for _, msg := range msgs {
			AddMessageToReply(ctx, reply, log.InfoLevel, msg)
		}

		return reply
	}
}

// NxDomain returns a ResponderFunc that replies with NXDOMAIN.
func NxDomain(msgs ...string) ResponderFunc {
	return func(ctx context.Context, request *dns.Msg) *dns.Msg {
		reply := new(dns.Msg).SetRcode(request, dns.RcodeNameError)
		for _, msg := range msgs {
			AddMessageToReply(ctx, reply, log.InfoLevel, msg)
		}
		return reply
	}
}

// Refused returns a ResponderFunc that replies with REFUSED.
func Refused(msgs ...string) ResponderFunc {
	return func(ctx context.Context, request *dns.Msg) *dns.Msg {
		reply := new(dns.Msg).SetRcode(request, dns.RcodeRefused)
		for _, msg := range msgs {
			AddMessageToReply(ctx, reply, log.InfoLevel, msg)
		}
		return reply
	}
}

// ServerFailure returns a ResponderFunc that replies with SERVFAIL.
func ServerFailure(msgs ...string) ResponderFunc {
	return func(ctx context.Context, request *dns.Msg) *dns.Msg {
		reply := new(dns.Msg).SetRcode(request, dns.RcodeServerFailure)
		for _, msg := range msgs {
			AddMessageToReply(ctx, reply, log.InfoLevel, msg)
		}
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
		return nil, errors.New("record is nil")
	}
	return rr, nil
}

// AddMessageToReply creates an information resource records using
// MakeMessageRecord and immediately adds it the the extra section of the given
// reply. If an error occurs, the resource record will not be added, and the
// error will be logged.
func AddMessageToReply(ctx context.Context, reply *dns.Msg, level log.Severity, msg string) {
	if msg != "" {
		rr, err := MakeMessageRecord(level, msg)
		if err != nil {
			log.Tracer(ctx).Warningf("nameserver: failed to add message to reply: %s", err)
			return
		}

		reply.Extra = append(reply.Extra, rr)
	}
}
