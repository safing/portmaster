// Fixtures for the Rust port of service/nameserver: the dns/listenAddress
// option, the nsutil responders and the listen address parsing.
package main

import (
	"context"
	"net"
	"strings"
	_ "unsafe" // Required for go:linkname.

	"github.com/miekg/dns"

	"github.com/safing/portmaster/base/config"
	"github.com/safing/portmaster/base/log"
	_ "github.com/safing/portmaster/service/nameserver" // Required for go:linkname.
	"github.com/safing/portmaster/service/nameserver/nsutil"
)

// registerNameserverConfig is the unexported registration of the nameserver
// package; linking to it avoids copying the option definition.
//
//go:linkname registerNameserverConfig github.com/safing/portmaster/service/nameserver.registerConfig
func registerNameserverConfig() error

//go:linkname getNameserverListenAddresses github.com/safing/portmaster/service/nameserver.getListenAddresses
func getNameserverListenAddresses(listenAddress string) (ip1, ip2 net.IP, port uint16, err error)

func genNameserver() {
	if err := registerNameserverConfig(); err != nil {
		fail(err)
	}
	var options []*config.Option
	for _, option := range config.ExportOptions() {
		if option.Key == "dns/listenAddress" {
			options = append(options, option)
		}
	}
	writeObject("nameserver_options.json", options)

	writeObject("nameserver.json", map[string]any{
		"responders":       nameserverResponders(),
		"message_records":  nameserverMessageRecords(),
		"listen_addresses": nameserverListenAddresses(),
	})
}

func rrSummary(rr dns.RR) map[string]any {
	h := rr.Header()
	out := map[string]any{
		"name":  h.Name,
		"ttl":   h.Ttl,
		"type":  dns.Type(h.Rrtype).String(),
		"class": dns.Class(h.Class).String(),
		"text":  rr.String(),
	}
	if txt, ok := rr.(*dns.TXT); ok {
		out["txt"] = txt.Txt
	}
	return out
}

func rrSummaries(rrs []dns.RR) []any {
	out := make([]any, 0, len(rrs))
	for _, rr := range rrs {
		out = append(out, rrSummary(rr))
	}
	return out
}

func replySummary(reply *dns.Msg) map[string]any {
	if reply == nil {
		return map[string]any{"nil": true}
	}
	questions := make([]any, 0, len(reply.Question))
	for _, q := range reply.Question {
		questions = append(questions, map[string]any{"name": q.Name, "type": q.Qtype, "class": q.Qclass})
	}
	return map[string]any{
		"nil":      false,
		"id":       reply.Id,
		"response": reply.Response,
		"opcode":   reply.Opcode,
		"rd":       reply.RecursionDesired,
		"ra":       reply.RecursionAvailable,
		"rcode":    reply.Rcode,
		"question": questions,
		"answer":   rrSummaries(reply.Answer),
		"ns":       rrSummaries(reply.Ns),
		"extra":    rrSummaries(reply.Extra),
	}
}

func nameserverResponders() []any {
	responders := map[string]nsutil.Responder{
		"block_ip":       nsutil.BlockIP("blocked by test"),
		"zero_ip":        nsutil.ZeroIP("zero"),
		"localhost":      nsutil.Localhost(),
		"nxdomain":       nsutil.NxDomain("nxdomain: no such thing"),
		"refused":        nsutil.Refused("unsupported qclass"),
		"server_failure": nsutil.ServerFailure("internal error: ", "second message"),
	}
	names := []string{"block_ip", "zero_ip", "localhost", "nxdomain", "refused", "server_failure"}

	questions := []struct {
		name  string
		qtype uint16
	}{
		{"example.com.", dns.TypeA},
		{"example.com.", dns.TypeAAAA},
		{"Example.COM.", dns.TypeA},
		{"example.com.", dns.TypeTXT},
		{"example.com.", dns.TypeMX},
	}

	var cases []any
	for _, name := range names {
		for _, q := range questions {
			request := new(dns.Msg)
			request.SetQuestion(q.name, q.qtype)
			request.Id = 4711
			request.RecursionDesired = true
			reply := responders[name].ReplyWithDNS(context.Background(), request)
			cases = append(cases, map[string]any{
				"responder": name,
				"qname":     q.name,
				"qtype":     q.qtype,
				"reply":     replySummary(reply),
			})
		}
	}

	// A request with two questions: the responders answer every question.
	request := new(dns.Msg)
	request.SetQuestion("example.com.", dns.TypeA)
	request.Question = append(request.Question, dns.Question{Name: "example.org.", Qtype: dns.TypeAAAA, Qclass: dns.ClassINET})
	request.Id = 7
	cases = append(cases, map[string]any{
		"responder": "block_ip",
		"qname":     "example.com.+example.org.",
		"qtype":     0,
		"reply":     replySummary(nsutil.BlockIP().ReplyWithDNS(context.Background(), request)),
	})
	return cases
}

func nameserverMessageRecords() []any {
	messages := []string{
		"simple",
		"with spaces and: colon",
		"semi;colon",
		`quote"inside`,
		`back\slash`,
		"tab\there",
		"unicode ключ",
		"",
		strings.Repeat("x", 300),
		"percent %s",
		"line\nbreak",
		"cr\rx",
		`tail\`,
		`esc\"quote`,
		`dec\065`,
		"nul\x00byte",
		"del\x7f",
	}
	var cases []any
	for _, level := range []log.Severity{log.InfoLevel, log.ErrorLevel} {
		for _, msg := range messages {
			rr, err := nsutil.MakeMessageRecord(level, msg)
			entry := map[string]any{"level": level.String(), "msg": msg}
			if err != nil {
				entry["error"] = true
			} else {
				entry["error"] = false
				entry["rr"] = rrSummary(rr)
			}
			cases = append(cases, entry)
		}
	}
	return cases
}

func nameserverListenAddresses() []any {
	inputs := []string{
		"localhost:53",
		"127.0.0.1:53",
		"0.0.0.0:53",
		"[::1]:5353",
		"[::]:53",
		"192.0.2.1:65535",
		"192.0.2.1:65536",
		"192.0.2.1:abc",
		"192.0.2.1",
		"nothost:53",
		"localhost:0",
		"",
	}
	var cases []any
	for _, in := range inputs {
		ip1, ip2, port, err := getNameserverListenAddresses(in)
		entry := map[string]any{"input": in, "error": err != nil}
		if err == nil {
			entry["ip1"] = ip1.String()
			entry["ip2"] = nil
			if ip2 != nil {
				entry["ip2"] = ip2.String()
			}
			entry["port"] = port
		}
		cases = append(cases, entry)
	}
	return cases
}
