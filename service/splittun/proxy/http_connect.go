package proxy

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
)

// httpConnectMaxResponseSize limits the size of the proxy's response header.
const httpConnectMaxResponseSize = 16 * 1024

// httpConnect performs an HTTP CONNECT handshake (RFC 9110, section 9.3.6) on
// conn, asking the proxy to relay it to host:port. host may be an IP address
// or a hostname. If user is set, basic authentication is used.
func httpConnect(conn net.Conn, user *url.Userinfo, host string, port uint16) error {
	target := net.JoinHostPort(host, strconv.Itoa(int(port)))

	var req bytes.Buffer
	fmt.Fprintf(&req, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n", target, target)
	if user != nil {
		password, _ := user.Password()
		credentials := base64.StdEncoding.EncodeToString([]byte(user.Username() + ":" + password))
		fmt.Fprintf(&req, "Proxy-Authorization: Basic %s\r\n", credentials)
	}
	req.WriteString("\r\n")
	if _, err := conn.Write(req.Bytes()); err != nil {
		return err
	}

	// Read the response header byte by byte, so that no data the proxy relays
	// right after the header is consumed: The tunnel uses conn directly.
	header, err := readHTTPHeader(conn)
	if err != nil {
		return err
	}
	resp, err := http.ReadResponse(bufio.NewReader(bytes.NewReader(header)), &http.Request{Method: http.MethodConnect})
	if err != nil {
		return fmt.Errorf("invalid response: %w", err)
	}
	_ = resp.Body.Close()

	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return nil
	case resp.StatusCode == http.StatusProxyAuthRequired && user == nil:
		return errors.New("proxy requires authentication")
	case resp.StatusCode == http.StatusProxyAuthRequired:
		return errors.New("authentication failed")
	default:
		return fmt.Errorf("proxy responded with %s", resp.Status)
	}
}

// readHTTPHeader reads from conn up to and including the empty line that
// terminates an HTTP header.
func readHTTPHeader(conn net.Conn) ([]byte, error) {
	header := make([]byte, 0, 256)
	b := make([]byte, 1)
	for len(header) < httpConnectMaxResponseSize {
		if _, err := io.ReadFull(conn, b); err != nil {
			return nil, err
		}
		header = append(header, b[0])
		if bytes.HasSuffix(header, []byte("\r\n\r\n")) {
			return header, nil
		}
	}
	return nil, errors.New("response header too large")
}
