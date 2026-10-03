package proxy

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
)

// SOCKS4 protocol constants, see https://www.openssh.com/txt/socks4.protocol
// and https://www.openssh.com/txt/socks4a.protocol.
const (
	socks4Version    = 0x04
	socks4CmdConnect = 0x01

	socks4ReplyVersion = 0x00
	socks4Granted      = 0x5A
)

var socks4ReplyErrors = map[byte]string{
	0x5B: "request rejected or failed",
	0x5C: "request rejected, the SOCKS server cannot connect to identd on the client",
	0x5D: "request rejected, the client program and identd report different user IDs",
}

// socks4Connect performs a SOCKS4 CONNECT handshake on conn, asking the
// server to relay it to host:port. If host is not an IP address, the SOCKS4a
// extension is used to let the server resolve it. SOCKS4 only supports IPv4
// destinations. The username of user, if set, is sent as the user ID.
func socks4Connect(conn net.Conn, user *url.Userinfo, host string, port uint16) error {
	var userID string
	if user != nil {
		userID = user.Username()
	}

	// Request: VN CD DSTPORT DSTIP USERID NULL [HOSTNAME NULL]
	req := []byte{socks4Version, socks4CmdConnect}
	req = binary.BigEndian.AppendUint16(req, port)
	if ip := net.ParseIP(host); ip != nil {
		ip4 := ip.To4()
		if ip4 == nil {
			return errors.New("SOCKS4 does not support IPv6 destinations")
		}
		req = append(req, ip4...)
		req = append(req, userID...)
		req = append(req, 0x00)
	} else {
		// SOCKS4a: an invalid IP of the form 0.0.0.x (x != 0) signals that
		// the hostname follows the user ID.
		req = append(req, 0, 0, 0, 1)
		req = append(req, userID...)
		req = append(req, 0x00)
		req = append(req, host...)
		req = append(req, 0x00)
	}
	if _, err := conn.Write(req); err != nil {
		return err
	}

	// Reply: VN CD DSTPORT DSTIP
	var resp [8]byte
	if _, err := io.ReadFull(conn, resp[:]); err != nil {
		return err
	}
	if resp[0] != socks4ReplyVersion {
		return fmt.Errorf("unexpected reply version %d", resp[0])
	}
	if resp[1] != socks4Granted {
		if msg, ok := socks4ReplyErrors[resp[1]]; ok {
			return errors.New(msg)
		}
		return fmt.Errorf("unknown reply code %d", resp[1])
	}
	return nil
}
