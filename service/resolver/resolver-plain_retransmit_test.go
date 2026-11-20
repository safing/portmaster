package resolver

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/miekg/dns"
)

// startScriptedDNSServer listens on a loopback UDP port and calls respond for
// every query it receives, with the number of the query (starting at 1). It
// sends whatever respond returns; nil sends nothing.
func startScriptedDNSServer(t *testing.T, respond func(n int, query *dns.Msg) []*dns.Msg) (addr string, queries *atomic.Int32) {
	t.Helper()

	pc, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %s", err)
	}
	t.Cleanup(func() { _ = pc.Close() })

	queries = new(atomic.Int32)
	go func() {
		buf := make([]byte, dns.MinMsgSize)
		for {
			n, from, err := pc.ReadFrom(buf)
			if err != nil {
				return // Closed by the cleanup.
			}
			query := new(dns.Msg)
			err = query.Unpack(buf[:n])
			if err != nil {
				continue
			}
			for _, reply := range respond(int(queries.Add(1)), query) {
				packed, err := reply.Pack()
				if err != nil {
					continue
				}
				_, _ = pc.WriteTo(packed, from)
			}
		}
	}()

	return pc.LocalAddr().String(), queries
}

func dialTestConn(t *testing.T, addr string) *dns.Conn {
	t.Helper()

	rawConn, err := net.Dial(networkUDP, addr)
	if err != nil {
		t.Fatalf("failed to dial: %s", err)
	}
	t.Cleanup(func() { _ = rawConn.Close() })
	return &dns.Conn{Conn: rawConn, UDPSize: 1024}
}

func testQuery() *dns.Msg {
	query := new(dns.Msg)
	query.SetQuestion("example.com.", dns.TypeA)
	return query
}

func TestExchangeRetransmitsLostQuery(t *testing.T) {
	t.Parallel()

	// Drop the first query, answer the second.
	addr, queries := startScriptedDNSServer(t, func(n int, query *dns.Msg) []*dns.Msg {
		if n == 1 {
			return nil
		}
		return []*dns.Msg{new(dns.Msg).SetReply(query)}
	})
	conn := dialTestConn(t, addr)

	query := testQuery()
	reply, _, err := exchangeWithRetransmit(context.Background(), conn, query, 2*time.Second, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("exchange failed: %s", err)
	}
	if reply.Id != query.Id {
		t.Errorf("reply has ID %d, expected %d", reply.Id, query.Id)
	}
	if got := queries.Load(); got != 2 {
		t.Errorf("server received %d queries, expected 2", got)
	}
}

func TestExchangeSkipsRepliesWithOtherIDs(t *testing.T) {
	t.Parallel()

	// Send a stale reply with another ID before the real one.
	addr, _ := startScriptedDNSServer(t, func(_ int, query *dns.Msg) []*dns.Msg {
		stale := new(dns.Msg).SetReply(query)
		stale.Id = query.Id + 1
		stale.Rcode = dns.RcodeServerFailure
		return []*dns.Msg{stale, new(dns.Msg).SetReply(query)}
	})
	conn := dialTestConn(t, addr)

	query := testQuery()
	reply, _, err := exchangeWithRetransmit(context.Background(), conn, query, 2*time.Second, time.Second)
	if err != nil {
		t.Fatalf("exchange failed: %s", err)
	}
	if reply.Id != query.Id || reply.Rcode != dns.RcodeSuccess {
		t.Errorf("got reply with ID %d and rcode %d, expected ID %d and success", reply.Id, reply.Rcode, query.Id)
	}
}

func TestExchangeTimesOutAfterRetransmits(t *testing.T) {
	t.Parallel()

	addr, queries := startScriptedDNSServer(t, func(int, *dns.Msg) []*dns.Msg { return nil })
	conn := dialTestConn(t, addr)

	started := time.Now()
	_, _, err := exchangeWithRetransmit(context.Background(), conn, testQuery(), 350*time.Millisecond, 100*time.Millisecond)
	took := time.Since(started)
	if !isTimeout(err) {
		t.Fatalf("expected a timeout error, got %v", err)
	}
	if took < 300*time.Millisecond || took > 2*time.Second {
		t.Errorf("exchange took %s, expected about 350ms", took)
	}
	// Sent at 0, 100, 200 and 300ms; allow for a slow scheduler.
	if got := queries.Load(); got < 3 {
		t.Errorf("server received %d queries, expected at least 3", got)
	}
}

func TestExchangeStopsOnContextCancel(t *testing.T) {
	t.Parallel()

	addr, _ := startScriptedDNSServer(t, func(int, *dns.Msg) []*dns.Msg { return nil })
	conn := dialTestConn(t, addr)

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(100*time.Millisecond, cancel)

	started := time.Now()
	_, _, err := exchangeWithRetransmit(ctx, conn, testQuery(), 5*time.Second, time.Second)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if took := time.Since(started); took > time.Second {
		t.Errorf("exchange took %s after the cancel", took)
	}
}
