package crew

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/safing/portmaster/service/intel"
	"github.com/safing/portmaster/service/network"
	"github.com/safing/portmaster/spn/cabin"
	"github.com/safing/portmaster/spn/conf"
	"github.com/safing/portmaster/spn/hub"
	"github.com/safing/portmaster/spn/navigator"
	"github.com/safing/portmaster/spn/terminal"
)

const (
	testPadding   = 8
	testQueueSize = 10
)

func TestConnectOp(t *testing.T) {
	t.Parallel()

	// This test cannot be made hermetic: the exit side of the connect op only
	// connects to IPs in the global scope (see ConnectOp.setup), so a local
	// listener on a loopback or LAN address is always denied. Running it
	// without network access would require a configurable scope check in
	// production code, which does not exist.
	if testing.Short() {
		t.Skip("skipping test in short mode: it connects to orf.at:80 on the Internet, " +
			"because the connect op only allows destinations with a global IP")
	}

	// Create test terminal pair.
	a, b, err := terminal.NewSimpleTestTerminalPair(0, 0,
		&terminal.TerminalOpts{
			FlowControl:     terminal.FlowControlDFQ,
			FlowControlSize: testQueueSize,
			Padding:         testPadding,
		},
	)
	if err != nil {
		t.Fatalf("failed to create test terminal pair: %s", err)
	}

	// The exit side of the connect op requires a terminal with a session for
	// rate limiting. Test terminals have none, so attach one to b.
	b.SetTerminalExtension(&sessionTestTerminal{TestTerminal: b})

	// Set up connect op.
	b.GrantPermission(terminal.MayConnect)
	conf.EnablePublicHub(true)
	identity, err := cabin.CreateIdentity(module.mgr.Ctx(), "test")
	if err != nil {
		t.Fatalf("failed to create identity: %s", err)
	}
	_, err = identity.MaintainAnnouncement(&hub.Announcement{
		Transports: []string{
			"tcp:17",
		},
		Exit: []string{
			"+ * */80",
			"- *",
		},
	}, true)
	if err != nil {
		t.Fatalf("failed to update identity: %s", err)
	}
	EnableConnecting(identity.Hub)
	{
		appConn, sluiceConn := net.Pipe()
		_, tErr := NewConnectOp(&Tunnel{
			connInfo: &network.Connection{
				Entity: (&intel.Entity{
					Protocol: 6,
					Port:     80,
					Domain:   "orf.at.",
					IP:       net.IPv4(194, 232, 104, 142),
				}).Init(0),
			},
			conn:        sluiceConn,
			dstTerminal: a,
			dstPin: &navigator.Pin{
				Hub: identity.Hub,
			},
		})
		if tErr != nil {
			t.Fatalf("failed to start connect op: %s", tErr)
		}

		// Send request.
		requestURL, err := url.Parse("http://orf.at/")
		if err != nil {
			t.Fatalf("failed to parse request url: %s", err)
		}
		r := http.Request{
			Method: http.MethodHead,
			URL:    requestURL,
		}
		err = r.Write(appConn)
		if err != nil {
			t.Fatalf("failed to write request: %s", err)
		}

		// Recv response.
		data := make([]byte, 1500)
		n, err := appConn.Read(data)
		if err != nil {
			t.Fatalf("failed to read request: %s", err)
		}
		if n == 0 {
			t.Fatal("received empty reply")
		}

		t.Log("received data:")
		fmt.Println(string(data[:n]))

		time.Sleep(500 * time.Millisecond)
	}
}

// sessionTestTerminal adds session support to a test terminal, as required by
// operations that rate limit per session, such as the connect op.
type sessionTestTerminal struct {
	*terminal.TestTerminal
	terminal.SessionAddOn
}
