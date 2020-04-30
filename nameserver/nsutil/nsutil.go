package nsutil

import (
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
	ReplyWithDNS(query *dns.Msg, reason string, reasonCtx interface{}) *dns.Msg
}

// RRProvider defines the interface that any block/deny reason interface
// may implement to support adding additional DNS resource records to
// the DNS responses extra (additional) section.
type RRProvider interface {
	// GetExtraRR is called when a DNS response to a DNS message is
	// crafted because the request is either denied or blocked.
	GetExtraRR(query *dns.Msg, reason string, reasonCtx interface{}) []dns.RR
}

// ResponderFunc is a convenience type to use a function
// directly as a Responder.
type ResponderFunc func(query *dns.Msg, reason string, reasonCtx interface{}) *dns.Msg

// ReplyWithDNS implements the Responder interface and calls rf.
func (rf ResponderFunc) ReplyWithDNS(query *dns.Msg, reason string, reasonCtx interface{}) *dns.Msg {
	return rf(query, reason, reasonCtx)
}

// ZeroIP is a ResponderFunc than replies with either 0.0.0.0 or :: for
// each A or AAAA question respectively.
func ZeroIP() ResponderFunc {
	return func(query *dns.Msg, _ string, _ interface{}) *dns.Msg {
		m := new(dns.Msg)
		hasErr := false

		for _, question := range query.Question {
			var rr dns.RR
			var err error

			switch question.Qtype {
			case dns.TypeA:
				rr, err = dns.NewRR(question.Name + "  0	IN	A		0.0.0.0")
			case dns.TypeAAAA:
				rr, err = dns.NewRR(question.Name + "  0	IN	AAAA	::")
			}

			if err != nil {
				log.Errorf("nameserver: failed to create zero-ip response for %s: %s", question.Name, err)
				hasErr = true
			} else {
				m.Answer = append(m.Answer, rr)
			}
		}

		if hasErr && len(m.Answer) == 0 {
			m.SetRcode(query, dns.RcodeServerFailure)
		} else {
			m.SetRcode(query, dns.RcodeSuccess)
		}

		return m
	}
}

// NxDomain returns a ResponderFunc that replies with NXDOMAIN.
func NxDomain() ResponderFunc {
	return func(query *dns.Msg, _ string, _ interface{}) *dns.Msg {
		return new(dns.Msg).SetRcode(query, dns.RcodeNameError)
	}
}

// Refused returns a ResponderFunc that replies with REFUSED.
func Refused() ResponderFunc {
	return func(query *dns.Msg, _ string, _ interface{}) *dns.Msg {
		return new(dns.Msg).SetRcode(query, dns.RcodeRefused)
	}
}

// ServeFail returns a ResponderFunc that replies with SERVFAIL.
func ServeFail() ResponderFunc {
	return func(query *dns.Msg, _ string, _ interface{}) *dns.Msg {
		return new(dns.Msg).SetRcode(query, dns.RcodeServerFailure)
	}
}
