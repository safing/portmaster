package nsutil

import (
	"context"
	"strings"
	"testing"

	"github.com/miekg/dns"

	"github.com/safing/portmaster/base/log"
)

// requestSeeds returns wire-format DNS requests covering common and unusual
// question shapes.
func requestSeeds(tb testing.TB) [][]byte {
	tb.Helper()

	var msgs []*dns.Msg
	add := func(name string, qtype uint16) *dns.Msg {
		m := new(dns.Msg).SetQuestion(name, qtype)
		msgs = append(msgs, m)
		return m
	}

	add("example.com.", dns.TypeA)
	add("Example.COM.", dns.TypeAAAA)
	add("localhost.", dns.TypeA)
	add("foo.localhost.", dns.TypeAAAA)
	add("example.com.", dns.TypeTXT)
	add(".", dns.TypeNS)
	add(`we\ ird\;na\"me\(\).localhost.`, dns.TypeA)
	add(`\000\255.localhost.`, dns.TypeAAAA)
	add(strings.Repeat("a", 63)+"."+strings.Repeat("b", 63)+"."+strings.Repeat("c", 63)+"."+strings.Repeat("d", 61)+".", dns.TypeA)
	add("edns.example.", dns.TypeA).SetEdns0(4096, true)
	m := add("multi.example.", dns.TypeA)
	m.Question = append(m.Question, dns.Question{Name: "second.example.", Qtype: dns.TypeAAAA, Qclass: dns.ClassINET})
	m = add("chaos.example.", dns.TypeA)
	m.Question[0].Qclass = dns.ClassCHAOS
	m = add("noquestion.example.", dns.TypeA)
	m.Question = nil

	seeds := make([][]byte, 0, len(msgs))
	for _, m := range msgs {
		data, err := m.Pack()
		if err != nil {
			tb.Fatalf("failed to pack seed: %s", err)
		}
		seeds = append(seeds, data)
	}
	return seeds
}

func FuzzResponders(f *testing.F) {
	for _, seed := range requestSeeds(f) {
		f.Add(seed, "message")
	}

	ctx := context.Background()

	f.Fuzz(func(t *testing.T, data []byte, msg string) {
		request := new(dns.Msg)
		err := request.Unpack(data)
		if err != nil {
			return
		}

		responders := map[string]ResponderFunc{
			"block":     BlockIP(msg),
			"zero":      ZeroIP(msg),
			"localhost": Localhost(msg),
			"refused":   Refused(msg),
			"servfail":  ServerFailure(msg),
			// NxDomain is not fuzzed, as it sleeps.
		}
		for name, responder := range responders {
			reply := responder.ReplyWithDNS(ctx, request)
			if reply == nil {
				t.Fatalf("%s: nil reply", name)
			}
			if reply.Id != request.Id || !reply.Response {
				t.Fatalf("%s: reply does not match request", name)
			}

			// Every answer must be for one of the questions.
			for _, rr := range reply.Answer {
				found := false
				for _, q := range request.Question {
					if rr.Header().Name == q.Name && rr.Header().Rrtype == q.Qtype {
						found = true
						break
					}
				}
				if !found {
					t.Fatalf("%s: answer %s does not match any question %v", name, rr, request.Question)
				}
			}

			_, err := reply.Pack()
			if err != nil {
				t.Fatalf("%s: failed to pack reply: %s", name, err)
			}
		}
	})
}

func FuzzMakeMessageRecord(f *testing.F) {
	for _, seed := range []string{
		"",
		"internal error: failed to check if request is local",
		`quote " inside`,
		`back\slash`,
		"new\nline",
		"semi;colon",
		strings.Repeat("x", 300),
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, msg string) {
		rr, err := MakeMessageRecord(log.InfoLevel, msg)
		if err != nil {
			return
		}
		if rr.Header().Name != strings.ToLower(log.InfoLevel.String())+".portmaster." || rr.Header().Rrtype != dns.TypeTXT {
			t.Fatalf("message %q produced unexpected record %s", msg, rr)
		}

		reply := new(dns.Msg)
		AddMessagesToReply(context.Background(), reply, log.InfoLevel, msg)
		_, err = reply.Pack()
		if err != nil {
			t.Fatalf("failed to pack reply with message %q: %s", msg, err)
		}
	})
}
