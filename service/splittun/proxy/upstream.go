package proxy

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// upstreamProtocol describes how to establish a relayed connection through
// one type of proxy server.
type upstreamProtocol struct {
	// name is the human readable protocol name, used in errors.
	name string

	// sendsHostname defines whether the destination hostname is sent to the
	// proxy instead of the destination IP, when the hostname is known.
	sendsHostname bool

	// allowsPassword defines whether the protocol can authenticate with a
	// password. Protocols without password support only accept a username.
	allowsPassword bool

	// connect performs the handshake on conn, which is connected to the proxy
	// server, asking it to relay the connection to host:port.
	// host is an IP address or, if sendsHostname is set, possibly a hostname.
	// user holds the optional credentials from the proxy URL.
	connect func(conn net.Conn, user *url.Userinfo, host string, port uint16) error
}

// upstreamProtocols holds the supported upstream proxy protocols by URL scheme.
//
// The schemes ending in "a" or "h" let the proxy resolve the destination
// hostname, following the convention established by curl. HTTP proxies
// always receive the hostname, if it is known.
var upstreamProtocols = map[string]upstreamProtocol{
	"http": {
		name:           "HTTP CONNECT",
		sendsHostname:  true,
		allowsPassword: true,
		connect:        httpConnect,
	},
	"socks4": {
		name:    "SOCKS4",
		connect: socks4Connect,
	},
	"socks4a": {
		name:          "SOCKS4a",
		sendsHostname: true,
		connect:       socks4Connect,
	},
	"socks5": {
		name:           "SOCKS5",
		allowsPassword: true,
		connect:        socks5Connect,
	},
	"socks5h": {
		name:           "SOCKS5",
		sendsHostname:  true,
		allowsPassword: true,
		connect:        socks5Connect,
	},
}

// SupportedUpstreamProxySchemes returns the supported upstream proxy URL
// schemes in alphabetical order.
func SupportedUpstreamProxySchemes() []string {
	schemes := make([]string, 0, len(upstreamProtocols))
	for scheme := range upstreamProtocols {
		schemes = append(schemes, scheme)
	}
	sort.Strings(schemes)
	return schemes
}

// UpstreamProxy describes a proxy server that outbound TCP sessions are
// relayed through instead of connecting to the destination directly.
type UpstreamProxy struct {
	// URL is the proxy server address, as returned by ParseUpstreamProxyURL.
	URL *url.URL

	// Host is the optional hostname of the destination. It is sent to the
	// proxy instead of the destination IP if the protocol supports it.
	Host string
}

// ParseUpstreamProxyURL parses and validates an upstream proxy URL, e.g.
// "socks5://127.0.0.1:1080" or "http://user:password@proxy.example:3128".
// See SupportedUpstreamProxySchemes for the supported schemes.
//
// Returned errors never contain the URL itself, as it may carry credentials.
func ParseUpstreamProxyURL(s string) (*url.URL, error) {
	u, err := url.Parse(s)
	if err != nil {
		return nil, errors.New("proxy URL is malformed")
	}

	protocol, ok := upstreamProtocols[u.Scheme]
	if !ok {
		return nil, fmt.Errorf(
			"unsupported proxy scheme %q, supported are: %s",
			u.Scheme, strings.Join(SupportedUpstreamProxySchemes(), ", "),
		)
	}

	if u.Hostname() == "" {
		return nil, errors.New("proxy URL is missing the host")
	}
	port, err := strconv.ParseUint(u.Port(), 10, 16)
	if err != nil || port == 0 {
		return nil, errors.New("proxy URL is missing a valid port")
	}
	if (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("proxy URL must not contain a path, query or fragment")
	}

	if u.User != nil {
		username := u.User.Username()
		password, hasPassword := u.User.Password()
		switch {
		case hasPassword && !protocol.allowsPassword:
			return nil, fmt.Errorf("%s does not support password authentication, only a user ID", protocol.name)
		case username == "":
			return nil, errors.New("proxy username must not be empty")
		case strings.ContainsRune(username, 0) || strings.ContainsRune(password, 0):
			return nil, errors.New("proxy username and password must not contain NUL characters")
		// The SOCKS5 username/password authentication (RFC 1929) limits both
		// fields to 255 bytes. Apply the same sane limit to all protocols.
		case len(username) > 255 || len(password) > 255:
			return nil, errors.New("proxy username and password must be at most 255 bytes long")
		}
	}

	return u, nil
}

// Redacted returns the proxy address without credentials, for logging and display.
func (u *UpstreamProxy) Redacted() string {
	return u.URL.Scheme + "://" + u.URL.Host
}

// resolve returns the address of the proxy server. A hostname is resolved
// with the system resolver, preferring an address of the same IP family as
// preferIP, if given.
func (u *UpstreamProxy) resolve(ctx context.Context, preferIP net.IP) (net.IP, uint16, error) {
	port, err := strconv.ParseUint(u.URL.Port(), 10, 16)
	if err != nil {
		return nil, 0, fmt.Errorf("invalid proxy port: %w", err)
	}

	host := u.URL.Hostname()
	if ip := net.ParseIP(host); ip != nil {
		return ip, uint16(port), nil
	}

	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to resolve proxy host: %w", err)
	}
	if len(addrs) == 0 {
		return nil, 0, errors.New("proxy host has no addresses")
	}
	if preferIP != nil {
		wantIPv4 := preferIP.To4() != nil
		for _, addr := range addrs {
			if (addr.IP.To4() != nil) == wantIPv4 {
				return addr.IP, uint16(port), nil
			}
		}
	}
	return addrs[0].IP, uint16(port), nil
}

// dial connects to the proxy server at proxyAddr using d and asks it to relay
// the connection to destIP:destPort.
//
// The returned connection is the plain connection to the proxy server, so
// that TCP half-close keeps working.
func (u *UpstreamProxy) dial(ctx context.Context, d *net.Dialer, proxyAddr string, destIP net.IP, destPort uint16) (net.Conn, error) {
	protocol, ok := upstreamProtocols[u.URL.Scheme]
	if !ok {
		return nil, fmt.Errorf("unsupported proxy scheme %q", u.URL.Scheme)
	}

	conn, err := d.DialContext(ctx, "tcp", proxyAddr)
	if err != nil {
		return nil, err
	}

	host := destIP.String()
	if protocol.sendsHostname {
		if hostname := strings.TrimSuffix(u.Host, "."); isValidHostname(hostname) {
			host = hostname
		}
	}

	err = withHandshakeContext(ctx, conn, func() error {
		return protocol.connect(conn, u.URL.User, host, destPort)
	})
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("%s handshake failed: %w", protocol.name, err)
	}
	return conn, nil
}

// withHandshakeContext runs handshake while applying the deadline of ctx to
// conn, and aborts pending I/O on conn if ctx is cancelled.
func withHandshakeContext(ctx context.Context, conn net.Conn, handshake func() error) (err error) {
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	stop := context.AfterFunc(ctx, func() {
		// Unblock any pending read or write.
		_ = conn.SetDeadline(time.Unix(1, 0))
	})
	defer func() {
		if !stop() && err == nil {
			// ctx was cancelled right after the handshake finished. The
			// deadline might be set to the past, so the connection is unusable.
			err = ctx.Err()
		}
		_ = conn.SetDeadline(time.Time{})
	}()

	return handshake()
}

// isValidHostname reports whether s is a plausible DNS hostname that can be
// safely embedded in any proxy protocol request.
func isValidHostname(s string) bool {
	if len(s) == 0 || len(s) > 253 {
		return false
	}
	for _, c := range s {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case c == '.', c == '-', c == '_':
		default:
			return false
		}
	}
	return true
}
