package nameserver

import (
	"context"
	"errors"
	"net"
	"testing"

	"github.com/miekg/dns"

	"github.com/safing/portmaster/service/nameserver/nsutil"
)

var errTestWrite = errors.New("test write error")

// testResponseWriter is a dns.ResponseWriter that fails the first failWrites
// writes and records every successfully written message.
type testResponseWriter struct {
	failWrites int
	written    []*dns.Msg
}

func (w *testResponseWriter) LocalAddr() net.Addr {
	return &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 53}
}

func (w *testResponseWriter) RemoteAddr() net.Addr {
	return &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 40000}
}

func (w *testResponseWriter) WriteMsg(m *dns.Msg) error {
	if w.failWrites > 0 {
		w.failWrites--
		return errTestWrite
	}
	w.written = append(w.written, m.Copy())
	return nil
}

func (w *testResponseWriter) Write([]byte) (int, error) { return 0, errTestWrite }
func (w *testResponseWriter) Close() error              { return nil }
func (w *testResponseWriter) TsigStatus() error         { return nil }
func (w *testResponseWriter) TsigTimersOnly(bool)       {}
func (w *testResponseWriter) Hijack()                   {}

func TestWriteDNSResponse(t *testing.T) {
	t.Parallel()

	newReply := func() *dns.Msg {
		reply := new(dns.Msg).SetRcode(new(dns.Msg).SetQuestion("example.com.", dns.TypeA), dns.RcodeRefused)
		nsutil.AddMessagesToReply(context.Background(), reply, 0, "some info")
		return reply
	}

	t.Run("first write succeeds", func(t *testing.T) {
		t.Parallel()

		w := &testResponseWriter{}
		err := writeDNSResponse(context.Background(), w, newReply())
		if err != nil {
			t.Fatalf("unexpected error: %s", err)
		}
		if len(w.written) != 1 || len(w.written[0].Extra) == 0 {
			t.Fatalf("expected one reply with extra section, got %v", w.written)
		}
	})

	t.Run("retry without extra section succeeds", func(t *testing.T) {
		t.Parallel()

		w := &testResponseWriter{failWrites: 1}
		err := writeDNSResponse(context.Background(), w, newReply())
		if err != nil {
			t.Fatalf("reply was sent without extra section, but an error was returned: %s", err)
		}
		if len(w.written) != 1 || len(w.written[0].Extra) != 0 {
			t.Fatalf("expected one reply without extra section, got %v", w.written)
		}
	})

	t.Run("both writes fail", func(t *testing.T) {
		t.Parallel()

		w := &testResponseWriter{failWrites: 2}
		err := writeDNSResponse(context.Background(), w, newReply())
		if !errors.Is(err, errTestWrite) {
			t.Fatalf("expected write error, got %v", err)
		}
		if len(w.written) != 0 {
			t.Fatalf("expected no reply, got %v", w.written)
		}
	})
}
