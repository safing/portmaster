package nsutil

import (
	"context"
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
func ZeroIP(msg string) ResponderFunc {
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

		AddMessageToReply(ctx, reply, log.InfoLevel, msg)

		return reply
	}
}

func Localhost(msg string) ResponderFunc {
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

		AddMessageToReply(ctx, reply, log.InfoLevel, msg)

		return reply
	}
}

// NxDomain returns a ResponderFunc that replies with NXDOMAIN.
func NxDomain(msg string) ResponderFunc {
	return func(ctx context.Context, request *dns.Msg) *dns.Msg {
		reply := new(dns.Msg).SetRcode(request, dns.RcodeNameError)
		AddMessageToReply(ctx, reply, log.InfoLevel, msg)
		return reply
	}
}

// Refused returns a ResponderFunc that replies with REFUSED.
func Refused(msg string) ResponderFunc {
	return func(ctx context.Context, request *dns.Msg) *dns.Msg {
		reply := new(dns.Msg).SetRcode(request, dns.RcodeRefused)
		AddMessageToReply(ctx, reply, log.InfoLevel, msg)
		return reply
	}
}

// ServerFailure returns a ResponderFunc that replies with SERVFAIL.
func ServerFailure(msg string) ResponderFunc {
	return func(ctx context.Context, request *dns.Msg) *dns.Msg {
		reply := new(dns.Msg).SetRcode(request, dns.RcodeServerFailure)
		AddMessageToReply(ctx, reply, log.InfoLevel, msg)
		return reply
	}
}

func MakeMessageRecord(level log.Severity, msg string) (dns.RR, error) {
	return dns.NewRR(fmt.Sprintf(
		`%s.portmaster. 0 IN TXT "%s"`,
		strings.ToLower(level.String()),
		msg,
	))
}

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
