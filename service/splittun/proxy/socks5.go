package proxy

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
)

// SOCKS5 protocol constants, see RFC 1928 and RFC 1929.
const (
	socks5Version = 0x05

	socks5AuthNone         = 0x00
	socks5AuthUserPass     = 0x02
	socks5AuthNoAcceptable = 0xFF

	socks5UserPassVersion = 0x01

	socks5CmdConnect = 0x01

	socks5AddrIPv4   = 0x01
	socks5AddrDomain = 0x03
	socks5AddrIPv6   = 0x04
)

var socks5ReplyErrors = map[byte]string{
	0x01: "general SOCKS server failure",
	0x02: "connection not allowed by ruleset",
	0x03: "network unreachable",
	0x04: "host unreachable",
	0x05: "connection refused",
	0x06: "TTL expired",
	0x07: "command not supported",
	0x08: "address type not supported",
}

// socks5Connect performs a SOCKS5 CONNECT handshake (RFC 1928) on conn,
// asking the server to relay it to host:port. host may be an IP address or a
// hostname. If user is not nil, username/password authentication (RFC 1929)
// is offered.
func socks5Connect(conn net.Conn, user *url.Userinfo, host string, port uint16) error {
	if err := socks5Authenticate(conn, user); err != nil {
		return err
	}

	// Request: VER CMD RSV ATYP DST.ADDR DST.PORT
	req := []byte{socks5Version, socks5CmdConnect, 0x00}
	if ip := net.ParseIP(host); ip != nil {
		if ip4 := ip.To4(); ip4 != nil {
			req = append(req, socks5AddrIPv4)
			req = append(req, ip4...)
		} else {
			req = append(req, socks5AddrIPv6)
			req = append(req, ip.To16()...)
		}
	} else {
		if len(host) == 0 || len(host) > 255 {
			return fmt.Errorf("invalid destination hostname length %d", len(host))
		}
		req = append(req, socks5AddrDomain, byte(len(host)))
		req = append(req, host...)
	}
	req = binary.BigEndian.AppendUint16(req, port)
	if _, err := conn.Write(req); err != nil {
		return err
	}

	// Reply: VER REP RSV ATYP BND.ADDR BND.PORT
	var hdr [4]byte
	if _, err := io.ReadFull(conn, hdr[:]); err != nil {
		return err
	}
	if hdr[0] != socks5Version {
		return fmt.Errorf("unexpected protocol version %d in reply", hdr[0])
	}
	if hdr[1] != 0x00 {
		if msg, ok := socks5ReplyErrors[hdr[1]]; ok {
			return errors.New(msg)
		}
		return fmt.Errorf("unknown reply code %d", hdr[1])
	}

	// Skip the bound address, which is not needed.
	var addrLen int
	switch hdr[3] {
	case socks5AddrIPv4:
		addrLen = net.IPv4len
	case socks5AddrIPv6:
		addrLen = net.IPv6len
	case socks5AddrDomain:
		var l [1]byte
		if _, err := io.ReadFull(conn, l[:]); err != nil {
			return err
		}
		addrLen = int(l[0])
	default:
		return fmt.Errorf("unknown address type %d in reply", hdr[3])
	}
	if _, err := io.ReadFull(conn, make([]byte, addrLen+2)); err != nil {
		return err
	}

	return nil
}

// socks5Authenticate negotiates the authentication method and authenticates
// with username/password if requested by the server.
func socks5Authenticate(conn net.Conn, user *url.Userinfo) error {
	// Greeting: VER NMETHODS METHODS
	greeting := []byte{socks5Version, 1, socks5AuthNone}
	if user != nil {
		greeting = []byte{socks5Version, 2, socks5AuthNone, socks5AuthUserPass}
	}
	if _, err := conn.Write(greeting); err != nil {
		return err
	}

	// Reply: VER METHOD
	var resp [2]byte
	if _, err := io.ReadFull(conn, resp[:]); err != nil {
		return err
	}
	if resp[0] != socks5Version {
		return fmt.Errorf("unexpected protocol version %d", resp[0])
	}

	switch resp[1] {
	case socks5AuthNone:
		return nil
	case socks5AuthUserPass:
		if user == nil {
			return errors.New("server requires username/password authentication")
		}
	case socks5AuthNoAcceptable:
		return errors.New("server accepts none of the offered authentication methods")
	default:
		return fmt.Errorf("server selected unsupported authentication method %d", resp[1])
	}

	// Username/password request: VER ULEN UNAME PLEN PASSWD
	username := user.Username()
	password, _ := user.Password()
	if len(username) == 0 || len(username) > 255 || len(password) > 255 {
		return errors.New("invalid username or password length")
	}
	req := []byte{socks5UserPassVersion, byte(len(username))}
	req = append(req, username...)
	req = append(req, byte(len(password)))
	req = append(req, password...)
	if _, err := conn.Write(req); err != nil {
		return err
	}

	// Reply: VER STATUS
	if _, err := io.ReadFull(conn, resp[:]); err != nil {
		return err
	}
	if resp[1] != 0x00 {
		return errors.New("authentication failed")
	}
	return nil
}
