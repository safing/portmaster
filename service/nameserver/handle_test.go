package nameserver

import (
	"context"
	"net"
	"strings"
	"sync"
	"testing"

	vm "github.com/VictoriaMetrics/metrics"
	"github.com/miekg/dns"

	"github.com/safing/portmaster/base/metrics"
	"github.com/safing/portmaster/service/intel"
	"github.com/safing/portmaster/service/nameserver/nsutil"
	"github.com/safing/portmaster/service/network"
	"github.com/safing/portmaster/service/resolver"
)

var registerTestMetricsOnce sync.Once

func setupHandleRequestTest(t *testing.T) {
	t.Helper()

	registerTestMetricsOnce.Do(func() {
		// The metrics module is not running, so use unregistered metrics.
		set := vm.NewSet()
		requestsHistogram = &metrics.Histogram{Histogram: set.NewHistogram("test_duration_seconds")}
		totalHandledRequests = &metrics.Counter{Counter: set.NewCounter("test_total")}
		hostname = "testhost."
	})
}

type tcpResponseWriter struct {
	testResponseWriter
}

func (w *tcpResponseWriter) RemoteAddr() net.Addr {
	return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 40000}
}

// TestHandleRequestEarlyReplies tests the request handling paths that reply
// before any connection or resolver is involved.
func TestHandleRequestEarlyReplies(t *testing.T) {
	t.Parallel()
	setupHandleRequestTest(t)

	q := func(name string, qtype uint16) dns.Question {
		return dns.Question{Name: name, Qtype: qtype, Qclass: dns.ClassINET}
	}

	tests := []struct {
		name      string
		questions []dns.Question
		tcp       bool
		rcode     int
		answers   int
	}{
		{name: "no question", rcode: dns.RcodeRefused},
		{name: "multiple questions", questions: []dns.Question{q("a.example.", dns.TypeA), q("b.example.", dns.TypeA)}, rcode: dns.RcodeRefused},
		{name: "chaos class", questions: []dns.Question{{Name: "version.bind.", Qtype: dns.TypeTXT, Qclass: dns.ClassCHAOS}}, rcode: dns.RcodeRefused},
		{name: "any class", questions: []dns.Question{{Name: "example.com.", Qtype: dns.TypeA, Qclass: dns.ClassANY}}, rcode: dns.RcodeRefused},
		{name: "tcp", questions: []dns.Question{q("example.com.", dns.TypeA)}, tcp: true, rcode: dns.RcodeRefused},
		{name: "localhost A", questions: []dns.Question{q("localhost.", dns.TypeA)}, rcode: dns.RcodeSuccess, answers: 1},
		{name: "localhost mixed case AAAA", questions: []dns.Question{q("Foo.LocalHost.", dns.TypeAAAA)}, rcode: dns.RcodeSuccess, answers: 1},
		{name: "hostname", questions: []dns.Question{q("testhost.", dns.TypeA)}, rcode: dns.RcodeSuccess, answers: 1},
		{name: "localhost MX", questions: []dns.Question{q("localhost.", dns.TypeMX)}, rcode: dns.RcodeNameError},
		{name: "localhost with odd characters", questions: []dns.Question{q(`we\ ird\"\;.localhost.`, dns.TypeA)}, rcode: dns.RcodeSuccess, answers: 1},
		{name: "invalid domain", questions: []dns.Question{q(`in\ valid.example.`, dns.TypeA)}, rcode: dns.RcodeRefused},
		{name: "empty name", questions: []dns.Question{q("", dns.TypeA)}, rcode: dns.RcodeRefused},
		{name: "too long name", questions: []dns.Question{q(strings.Repeat("a.", 200), dns.TypeA)}, rcode: dns.RcodeRefused},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			request := new(dns.Msg)
			request.Id = 1234
			request.RecursionDesired = true
			request.Question = tc.questions
			request.SetEdns0(4096, false)

			var w dns.ResponseWriter
			rw := &testResponseWriter{}
			w = rw
			if tc.tcp {
				tw := &tcpResponseWriter{}
				rw = &tw.testResponseWriter
				w = tw
			}

			err := handleRequest(context.Background(), w, request)
			if err != nil {
				t.Fatalf("handleRequest failed: %s", err)
			}
			if len(rw.written) != 1 {
				t.Fatalf("expected one reply, got %d", len(rw.written))
			}
			reply := rw.written[0]
			if reply.Id != request.Id || !reply.Response || reply.Rcode != tc.rcode || len(reply.Answer) != tc.answers {
				t.Fatalf("unexpected reply: %s", reply)
			}
			for _, rr := range reply.Answer {
				if rr.Header().Name != tc.questions[0].Name {
					t.Fatalf("answer name %q does not match question %q", rr.Header().Name, tc.questions[0].Name)
				}
			}
			_, err = reply.Pack()
			if err != nil {
				t.Fatalf("failed to pack reply: %s", err)
			}
		})
	}
}

// TestReplyResolvedWithoutProfile tests that a resolved response is answered
// with a server failure when the firewall cannot decide on it, because the
// process has no profile. This happens when the profile could not be loaded and
// the connection was accepted without a profile, eg. because the privacy
// filter is disabled or an external verdict handler accepted it.
func TestReplyResolvedWithoutProfile(t *testing.T) {
	t.Parallel()
	setupHandleRequestTest(t)

	for _, nonStandard := range []bool{false, true} {
		request := new(dns.Msg)
		request.SetQuestion("Example.org.", dns.TypeA)
		w := &testResponseWriter{}
		reply := func(responder nsutil.Responder, rrProviders ...nsutil.RRProvider) error {
			return sendResponse(context.Background(), w, request, responder, rrProviders...)
		}

		// Connection without process and profile, accepted by the privacy filter.
		conn := &network.Connection{
			Entity:  &intel.Entity{},
			Verdict: network.VerdictAccept,
		}
		q := &resolver.Query{FQDN: "example.org.", QType: dns.Type(dns.TypeA)}
		rrCache := &resolver.RRCache{
			Domain:   q.FQDN,
			Question: q.QType,
			RCode:    dns.RcodeSuccess,
			Answer: []dns.RR{&dns.A{
				Hdr: dns.RR_Header{Name: q.FQDN, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 60},
				A:   net.IPv4(192, 0, 2, 1),
			}},
			Resolver: &resolver.ResolverInfo{Type: resolver.ServerTypeDNS},
		}

		sent, err := replyResolved(context.Background(), reply, conn, q, rrCache, "Example.org.", nonStandard)
		if err != nil {
			t.Fatalf("replyResolved failed: %s", err)
		}
		if sent != nil {
			t.Fatalf("expected no response to be attached to the connection, got %+v", sent)
		}
		if len(w.written) != 1 || w.written[0].Rcode != dns.RcodeServerFailure {
			t.Fatalf("expected one server failure reply, got %v", w.written)
		}
	}
}
