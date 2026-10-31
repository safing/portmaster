package proxy

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"io"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

// testProxyServer is a minimal proxy server for tests. It speaks the protocol
// of the given URL scheme and relays accepted connections to target.
type testProxyServer struct {
	scheme string
	addr   string
	// user and password are required from clients if user is set.
	// SOCKS4 only checks the user ID.
	user, password string
	// reject makes the server refuse all relay requests.
	reject bool
	// banner is written to the client right after the handshake response,
	// in the same write, to simulate server-first protocols.
	banner []byte
	// target is where all accepted requests are relayed to.
	target string
	// requests receives the requested destination ("host:port") of each request.
	requests chan string
}

func startTestProxyServer(t *testing.T, srv *testProxyServer) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("proxy server listen: %v", err)
	}
	srv.addr = ln.Addr().String()
	srv.requests = make(chan string, 16)
	t.Cleanup(func() { ln.Close() })

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go srv.handle(conn)
		}
	}()
}

func (srv *testProxyServer) handle(conn net.Conn) {
	defer conn.Close()

	var success []byte
	var ok bool
	switch srv.scheme {
	case "http":
		success, ok = srv.handshakeHTTP(conn)
	case "socks4", "socks4a":
		success, ok = srv.handshakeSOCKS4(conn)
	case "socks5", "socks5h":
		success, ok = srv.handshakeSOCKS5(conn)
	}
	if !ok {
		return
	}

	upstream, err := net.Dial("tcp", srv.target)
	if err != nil {
		return
	}
	defer upstream.Close()
	if _, err := conn.Write(append(success, srv.banner...)); err != nil {
		return
	}

	go func() { _, _ = io.Copy(upstream, conn) }()
	_, _ = io.Copy(conn, upstream)
}

// handshakeHTTP handles an HTTP CONNECT request and returns the success
// response to send, or false if the request was answered with an error.
func (srv *testProxyServer) handshakeHTTP(conn net.Conn) ([]byte, bool) {
	req, err := http.ReadRequest(bufio.NewReader(conn))
	if err != nil || req.Method != http.MethodConnect {
		return nil, false
	}
	srv.requests <- req.Host

	if srv.user != "" {
		want := "Basic " + base64.StdEncoding.EncodeToString([]byte(srv.user+":"+srv.password))
		if req.Header.Get("Proxy-Authorization") != want {
			_, _ = conn.Write([]byte("HTTP/1.1 407 Proxy Authentication Required\r\nProxy-Authenticate: Basic\r\nContent-Length: 0\r\n\r\n"))
			return nil, false
		}
	}
	if srv.reject {
		_, _ = conn.Write([]byte("HTTP/1.1 403 Forbidden\r\nContent-Length: 0\r\n\r\n"))
		return nil, false
	}
	return []byte("HTTP/1.1 200 Connection established\r\nVia: test\r\n\r\n"), true
}

// handshakeSOCKS4 handles a SOCKS4(a) CONNECT request.
func (srv *testProxyServer) handshakeSOCKS4(conn net.Conn) ([]byte, bool) {
	r := bufio.NewReader(conn)
	var hdr [8]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil || hdr[0] != socks4Version || hdr[1] != socks4CmdConnect {
		return nil, false
	}
	port := binary.BigEndian.Uint16(hdr[2:4])
	userID, err := r.ReadString(0)
	if err != nil {
		return nil, false
	}
	userID = strings.TrimSuffix(userID, "\x00")

	host := net.IP(hdr[4:8]).String()
	if hdr[4] == 0 && hdr[5] == 0 && hdr[6] == 0 && hdr[7] != 0 {
		hostname, err := r.ReadString(0)
		if err != nil {
			return nil, false
		}
		host = strings.TrimSuffix(hostname, "\x00")
	}
	srv.requests <- net.JoinHostPort(host, strconv.Itoa(int(port)))

	if srv.reject || (srv.user != "" && userID != srv.user) {
		_, _ = conn.Write([]byte{socks4ReplyVersion, 0x5B, 0, 0, 0, 0, 0, 0})
		return nil, false
	}
	return []byte{socks4ReplyVersion, socks4Granted, 0, 0, 0, 0, 0, 0}, true
}

// handshakeSOCKS5 handles a SOCKS5 CONNECT request.
func (srv *testProxyServer) handshakeSOCKS5(conn net.Conn) ([]byte, bool) {
	// Greeting.
	var hdr [2]byte
	if _, err := io.ReadFull(conn, hdr[:]); err != nil {
		return nil, false
	}
	methods := make([]byte, hdr[1])
	if _, err := io.ReadFull(conn, methods); err != nil {
		return nil, false
	}
	method := byte(socks5AuthNone)
	if srv.user != "" {
		method = socks5AuthUserPass
	}
	if !bytes.Contains(methods, []byte{method}) {
		_, _ = conn.Write([]byte{socks5Version, socks5AuthNoAcceptable})
		return nil, false
	}
	_, _ = conn.Write([]byte{socks5Version, method})

	// Username/password.
	if method == socks5AuthUserPass {
		readField := func() string {
			var l [1]byte
			_, _ = io.ReadFull(conn, l[:])
			b := make([]byte, l[0])
			_, _ = io.ReadFull(conn, b)
			return string(b)
		}
		var ver [1]byte
		if _, err := io.ReadFull(conn, ver[:]); err != nil {
			return nil, false
		}
		user, password := readField(), readField()
		if user != srv.user || password != srv.password {
			_, _ = conn.Write([]byte{socks5UserPassVersion, 0x01})
			return nil, false
		}
		_, _ = conn.Write([]byte{socks5UserPassVersion, 0x00})
	}

	// Request.
	var req [4]byte
	if _, err := io.ReadFull(conn, req[:]); err != nil {
		return nil, false
	}
	var host string
	switch req[3] {
	case socks5AddrIPv4, socks5AddrIPv6:
		ip := make(net.IP, net.IPv4len)
		if req[3] == socks5AddrIPv6 {
			ip = make(net.IP, net.IPv6len)
		}
		_, _ = io.ReadFull(conn, ip)
		host = ip.String()
	case socks5AddrDomain:
		var l [1]byte
		_, _ = io.ReadFull(conn, l[:])
		b := make([]byte, l[0])
		_, _ = io.ReadFull(conn, b)
		host = string(b)
	}
	var port [2]byte
	if _, err := io.ReadFull(conn, port[:]); err != nil {
		return nil, false
	}
	srv.requests <- net.JoinHostPort(host, strconv.Itoa(int(binary.BigEndian.Uint16(port[:]))))

	if srv.reject {
		_, _ = conn.Write([]byte{socks5Version, 0x02, 0x00, socks5AddrIPv4, 0, 0, 0, 0, 0, 0})
		return nil, false
	}
	return []byte{socks5Version, 0x00, 0x00, socks5AddrIPv4, 127, 0, 0, 1, 0, 0}, true
}

// upstreamDecider routes every session to dest through the given upstream proxy.
func upstreamDecider(t *testing.T, dest string, proxyURL string, host string) DeciderFunc {
	t.Helper()
	addr, err := net.ResolveTCPAddr("tcp", dest)
	if err != nil {
		t.Fatalf("resolve dest: %v", err)
	}
	u, err := ParseUpstreamProxyURL(proxyURL)
	if err != nil {
		t.Fatalf("parse proxy URL: %v", err)
	}
	return func(_, _ net.Addr) (net.IP, uint16, *LocalBinding, any, error) {
		return addr.IP, uint16(addr.Port), &LocalBinding{Upstream: &UpstreamProxy{URL: u, Host: host}}, nil, nil
	}
}

// startUpstreamTCPProxy starts a TCP proxy with decider and returns a client
// connection to it.
func startUpstreamTCPProxy(t *testing.T, decider DeciderFunc) (*TCPProxy, net.Conn) {
	t.Helper()
	p, err := NewTCPProxy("127.0.0.1:0", "tcp4", decider, nil, "test")
	if err != nil {
		t.Fatalf("NewTCPProxy: %v", err)
	}
	t.Cleanup(func() { p.Shutdown(t.Context()) })

	conn, err := net.Dial("tcp", p.Addr().String())
	if err != nil {
		t.Fatalf("dial proxy: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	return p, conn
}

// echoThroughProxy sends msg through a TCP proxy using decider and returns
// the echoed reply.
func echoThroughProxy(t *testing.T, decider DeciderFunc, msg []byte) ([]byte, error) {
	t.Helper()
	_, conn := startUpstreamTCPProxy(t, decider)
	if _, err := conn.Write(msg); err != nil {
		return nil, err
	}
	reply := make([]byte, len(msg))
	_, err := io.ReadFull(conn, reply)
	return reply, err
}

func TestUpstreamProtocols(t *testing.T) {
	echoAddr, stopEcho := startTCPEchoServer(t)
	defer stopEcho()
	_, echoPort, _ := net.SplitHostPort(echoAddr)
	hostnameDest := net.JoinHostPort("example.com", echoPort)

	tests := []struct {
		scheme string
		// wantRequest is the destination the proxy must receive when the
		// destination hostname is known.
		wantRequest string
	}{
		{scheme: "http", wantRequest: hostnameDest},
		{scheme: "socks4", wantRequest: echoAddr},
		{scheme: "socks4a", wantRequest: hostnameDest},
		{scheme: "socks5", wantRequest: echoAddr},
		{scheme: "socks5h", wantRequest: hostnameDest},
	}
	for _, tc := range tests {
		t.Run(tc.scheme, func(t *testing.T) {
			srv := &testProxyServer{scheme: tc.scheme, target: echoAddr}
			startTestProxyServer(t, srv)
			proxyURL := tc.scheme + "://" + srv.addr

			// Relay with a known hostname.
			msg := []byte("hello through " + tc.scheme)
			reply, err := echoThroughProxy(t, upstreamDecider(t, echoAddr, proxyURL, "example.com."), msg)
			if err != nil {
				t.Fatalf("echo: %v", err)
			}
			if !bytes.Equal(reply, msg) {
				t.Fatalf("got %q, want %q", reply, msg)
			}
			if got := <-srv.requests; got != tc.wantRequest {
				t.Errorf("proxy got request for %q, want %q", got, tc.wantRequest)
			}

			// Without a hostname, or with an unsafe one, the IP is sent.
			for _, host := range []string{"", "bad host\r\nX-Injected: 1"} {
				if _, err := echoThroughProxy(t, upstreamDecider(t, echoAddr, proxyURL, host), msg); err != nil {
					t.Fatalf("echo with host %q: %v", host, err)
				}
				if got := <-srv.requests; got != echoAddr {
					t.Errorf("with host %q, proxy got request for %q, want %q", host, got, echoAddr)
				}
			}
		})
	}
}

func TestUpstreamRejected(t *testing.T) {
	echoAddr, stopEcho := startTCPEchoServer(t)
	defer stopEcho()

	for _, scheme := range SupportedUpstreamProxySchemes() {
		t.Run(scheme, func(t *testing.T) {
			srv := &testProxyServer{scheme: scheme, target: echoAddr, reject: true}
			startTestProxyServer(t, srv)

			if _, err := echoThroughProxy(t, upstreamDecider(t, echoAddr, scheme+"://"+srv.addr, ""), []byte("x")); err == nil {
				t.Fatal("expected failure when the proxy rejects the request")
			}
		})
	}
}

func TestUpstreamAuthentication(t *testing.T) {
	echoAddr, stopEcho := startTCPEchoServer(t)
	defer stopEcho()

	tests := []struct {
		scheme             string
		goodUser, badUser  string
		requiresNoPassword bool
	}{
		{scheme: "http", goodUser: "user:secret", badUser: "user:wrong"},
		{scheme: "socks4", goodUser: "user", badUser: "other", requiresNoPassword: true},
		{scheme: "socks5", goodUser: "user:secret", badUser: "user:wrong"},
	}
	for _, tc := range tests {
		t.Run(tc.scheme, func(t *testing.T) {
			srv := &testProxyServer{scheme: tc.scheme, target: echoAddr, user: "user", password: "secret"}
			if tc.requiresNoPassword {
				srv.password = ""
			}
			startTestProxyServer(t, srv)

			msg := []byte("authenticated")
			reply, err := echoThroughProxy(t, upstreamDecider(t, echoAddr, tc.scheme+"://"+tc.goodUser+"@"+srv.addr, ""), msg)
			if err != nil {
				t.Fatalf("echo with valid credentials: %v", err)
			}
			if !bytes.Equal(reply, msg) {
				t.Fatalf("got %q, want %q", reply, msg)
			}

			if _, err := echoThroughProxy(t, upstreamDecider(t, echoAddr, tc.scheme+"://"+tc.badUser+"@"+srv.addr, ""), msg); err == nil {
				t.Error("expected failure with invalid credentials")
			}
			if _, err := echoThroughProxy(t, upstreamDecider(t, echoAddr, tc.scheme+"://"+srv.addr, ""), msg); err == nil {
				t.Error("expected failure without credentials")
			}
		})
	}
}

// TestUpstreamServerFirstData checks that data sent by the destination right
// after the handshake response is not lost.
func TestUpstreamServerFirstData(t *testing.T) {
	echoAddr, stopEcho := startTCPEchoServer(t)
	defer stopEcho()

	for _, scheme := range SupportedUpstreamProxySchemes() {
		t.Run(scheme, func(t *testing.T) {
			banner := []byte("220 ready\r\n")
			srv := &testProxyServer{scheme: scheme, target: echoAddr, banner: banner}
			startTestProxyServer(t, srv)

			_, conn := startUpstreamTCPProxy(t, upstreamDecider(t, echoAddr, scheme+"://"+srv.addr, ""))
			got := make([]byte, len(banner))
			if _, err := io.ReadFull(conn, got); err != nil {
				t.Fatalf("read banner: %v", err)
			}
			if !bytes.Equal(got, banner) {
				t.Fatalf("got %q, want %q", got, banner)
			}
		})
	}
}

func TestUpstreamEgressTracking(t *testing.T) {
	echoAddr, stopEcho := startTCPEchoServer(t)
	defer stopEcho()

	srv := &testProxyServer{scheme: "socks5", target: echoAddr}
	startTestProxyServer(t, srv)

	p, conn := startUpstreamTCPProxy(t, upstreamDecider(t, echoAddr, "socks5://"+srv.addr, ""))
	if _, err := conn.Write([]byte("x")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := io.ReadFull(conn, make([]byte, 1)); err != nil {
		t.Fatalf("read: %v", err)
	}

	proxyAddr, _ := net.ResolveTCPAddr("tcp", srv.addr)
	destAddr, _ := net.ResolveTCPAddr("tcp", echoAddr)
	if !p.HasProxiedEgressConnection(proxyAddr.IP, uint16(proxyAddr.Port)) {
		t.Error("egress connection to the upstream proxy is not tracked")
	}
	if p.HasProxiedEgressConnection(destAddr.IP, uint16(destAddr.Port)) {
		t.Error("destination is tracked as egress although the proxy is used")
	}
}

// TestUpstreamAddressFallback checks that all addresses of the proxy server
// are tried, e.g. when "localhost" resolves to "::1" first, but the proxy
// only listens on "127.0.0.1".
func TestUpstreamAddressFallback(t *testing.T) {
	echoAddr, stopEcho := startTCPEchoServer(t)
	defer stopEcho()

	srv := &testProxyServer{scheme: "socks5", target: echoAddr}
	startTestProxyServer(t, srv)
	proxyAddr, _ := net.ResolveTCPAddr("tcp", srv.addr)
	destAddr, _ := net.ResolveTCPAddr("tcp", echoAddr)

	p, err := NewTCPProxy("127.0.0.1:0", "tcp4", refuseDecider, nil, "test")
	if err != nil {
		t.Fatalf("NewTCPProxy: %v", err)
	}
	defer p.Shutdown(t.Context())

	u, _ := ParseUpstreamProxyURL("socks5://localhost:" + strconv.Itoa(proxyAddr.Port))
	upstream := &UpstreamProxy{URL: u}
	connCtx := newConnContext(nextID(), nil, destAddr.IP, uint16(destAddr.Port), func() {}, nil)
	connCtx.egressIP, connCtx.egressPort = net.IPv6loopback, uint16(proxyAddr.Port)
	p.cache.add(connCtx)
	defer p.cache.remove(connCtx)

	conn, err := p.dialUpstream(connCtx, upstream, nil, []net.IP{net.IPv6loopback, proxyAddr.IP}, uint16(proxyAddr.Port), destAddr.IP, uint16(destAddr.Port))
	if err != nil {
		t.Fatalf("dialUpstream: %v", err)
	}
	defer conn.Close()

	if !p.HasProxiedEgressConnection(proxyAddr.IP, uint16(proxyAddr.Port)) {
		t.Error("egress is not tracked for the address that was used")
	}
	if p.HasProxiedEgressConnection(net.IPv6loopback, uint16(proxyAddr.Port)) {
		t.Error("egress is still tracked for the failed address")
	}
}

func TestSortByIPVersion(t *testing.T) {
	v4a, v4b := net.ParseIP("192.0.2.1"), net.ParseIP("192.0.2.2")
	v6a, v6b := net.ParseIP("2001:db8::1"), net.ParseIP("2001:db8::2")

	tests := []struct {
		name     string
		preferIP net.IP
		want     []net.IP
	}{
		{name: "no preference", preferIP: nil, want: []net.IP{v4a, v4b, v6a, v6b}},
		{name: "prefer IPv4", preferIP: net.ParseIP("10.0.0.1"), want: []net.IP{v4a, v4b, v6a, v6b}},
		{name: "prefer IPv6", preferIP: net.ParseIP("fd00::1"), want: []net.IP{v6a, v6b, v4a, v4b}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ips := []net.IP{v6a, v4a, v6b, v4b}
			sortByIPVersion(ips, tc.preferIP)
			if !slices.EqualFunc(ips, tc.want, net.IP.Equal) {
				t.Errorf("got %v, want %v", ips, tc.want)
			}
		})
	}
}

func TestUpstreamResolveIPLiteral(t *testing.T) {
	u, _ := ParseUpstreamProxyURL("socks5://[::1]:1080")
	ips, port, err := (&UpstreamProxy{URL: u}).resolve(t.Context(), net.ParseIP("192.0.2.1"))
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if port != 1080 || len(ips) != 1 || !ips[0].Equal(net.IPv6loopback) {
		t.Errorf("got %v %d, want [::1] 1080", ips, port)
	}
}

func TestNewDialerLocalAddress(t *testing.T) {
	p, err := NewTCPProxy("127.0.0.1:0", "tcp4", refuseDecider, nil, "test")
	if err != nil {
		t.Fatalf("NewTCPProxy: %v", err)
	}
	defer p.Shutdown(t.Context())

	localV4, localV6 := net.ParseIP("192.0.2.1"), net.ParseIP("2001:db8::1")
	remoteV4, remoteV6 := net.ParseIP("198.51.100.1"), net.ParseIP("2001:db8::99")
	localAddr := func(d *net.Dialer) net.IP {
		if d.LocalAddr == nil {
			return nil
		}
		return d.LocalAddr.(*net.TCPAddr).IP
	}

	// The local address matches the IP version of the remote address.
	binding := &LocalBinding{IP: localV6, AltIP: localV4}
	for remote, want := range map[*net.IP]net.IP{&remoteV4: localV4, &remoteV6: localV6} {
		d, err := p.newDialer(binding, *remote)
		if err != nil {
			t.Fatalf("newDialer(%v): %v", *remote, err)
		}
		if got := localAddr(d); !got.Equal(want) {
			t.Errorf("newDialer(%v) binds to %v, want %v", *remote, got, want)
		}
	}

	// Without a local address of the remote's IP version, dialing must fail.
	if _, err := p.newDialer(&LocalBinding{IP: localV6}, remoteV4); err == nil {
		t.Error("expected error without a local IPv4 address")
	}

	// Without a binding, the OS chooses.
	if d, err := p.newDialer(nil, remoteV4); err != nil || d.LocalAddr != nil {
		t.Errorf("unexpected binding without LocalBinding: %v, %v", d, err)
	}
}

func TestSOCKS4RejectsIPv6(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	if err := socks4Connect(client, nil, "2001:db8::1", 443); err == nil {
		t.Fatal("expected error for IPv6 destination")
	}
}

func TestUDPRejectsUpstream(t *testing.T) {
	u, _ := ParseUpstreamProxyURL("socks5://127.0.0.1:1080")
	decider := func(_, _ net.Addr) (net.IP, uint16, *LocalBinding, any, error) {
		return net.IPv4(127, 0, 0, 1), 9, &LocalBinding{Upstream: &UpstreamProxy{URL: u}}, nil, nil
	}
	p, err := NewUDPProxy("127.0.0.1:0", "udp4", decider, nil, "test")
	if err != nil {
		t.Fatalf("NewUDPProxy: %v", err)
	}
	defer p.Shutdown(t.Context())

	conn, err := net.Dial("udp", p.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	if _, err := conn.Write([]byte("x")); err != nil {
		t.Fatalf("write: %v", err)
	}
	time.Sleep(100 * time.Millisecond)
	if n := p.Metrics().TotalCreated; n != 0 {
		t.Fatalf("created %d UDP sessions through an upstream proxy, want 0", n)
	}
}

func TestParseUpstreamProxyURL(t *testing.T) {
	valid := []string{
		"http://127.0.0.1:3128",
		"http://user:pass@proxy.example:3128",
		"socks4://127.0.0.1:1080",
		"socks4://userid@127.0.0.1:1080",
		"socks4a://proxy.example:1080",
		"socks5://127.0.0.1:1080",
		"socks5h://proxy.example:1080",
		"socks5://user:pass@[::1]:1080",
		"socks5://user@127.0.0.1:1080/",
	}
	for _, s := range valid {
		if _, err := ParseUpstreamProxyURL(s); err != nil {
			t.Errorf("ParseUpstreamProxyURL(%q): unexpected error: %v", s, err)
		}
	}

	invalid := []string{
		"",
		"127.0.0.1:1080",
		"https://127.0.0.1:8080",
		"socks4://user:pass@127.0.0.1:1080",
		"socks4a://user:pass@127.0.0.1:1080",
		"socks5://127.0.0.1",
		"socks5://127.0.0.1:0",
		"socks5://127.0.0.1:70000",
		"socks5://:1080",
		"socks5://127.0.0.1:1080/path",
		"socks5://127.0.0.1:1080?x=1",
		"socks5://:pass@127.0.0.1:1080",
		"socks5://us%00er@127.0.0.1:1080",
		"socks5://" + strings.Repeat("u", 256) + "@127.0.0.1:1080",
	}
	for _, s := range invalid {
		if _, err := ParseUpstreamProxyURL(s); err == nil {
			t.Errorf("ParseUpstreamProxyURL(%q): expected error", s)
		}
	}

	// Errors must not leak credentials.
	_, err := ParseUpstreamProxyURL("socks5://user:topsecret@127.0.0.1:1080/x")
	if err == nil || strings.Contains(err.Error(), "topsecret") {
		t.Errorf("unexpected error %v", err)
	}
	if got := (&UpstreamProxy{URL: &url.URL{Scheme: "socks5", User: url.UserPassword("u", "p"), Host: "h:1"}}).Redacted(); got != "socks5://h:1" {
		t.Errorf("Redacted() = %q", got)
	}
}
