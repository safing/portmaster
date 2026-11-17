//go:build linux

package nfq

import (
	"encoding/binary"
	"errors"
	"net"
	"strings"
	"testing"

	ct "github.com/florianl/go-conntrack"
	"github.com/mdlayher/netlink"
	"golang.org/x/sys/unix"
)

// Conntrack netlink constants (include/uapi/linux/netfilter/nfnetlink_conntrack.h).
const (
	fakeCtMsgNew    = 0 // IPCTNL_MSG_CT_NEW
	fakeCtMsgGet    = 1 // IPCTNL_MSG_CT_GET
	fakeCtMsgDelete = 2 // IPCTNL_MSG_CT_DELETE

	fakeCtaTupleOrig = 1 // CTA_TUPLE_ORIG
	fakeCtaTupleIP   = 1 // CTA_TUPLE_IP
	fakeCtaTupleProt = 2 // CTA_TUPLE_PROTO
	fakeCtaIPv4Src   = 1 // CTA_IP_V4_SRC
	fakeCtaIPv4Dst   = 2 // CTA_IP_V4_DST
	fakeCtaProtoNum  = 1 // CTA_PROTO_NUM
	fakeCtaSrcPort   = 2 // CTA_PROTO_SRC_PORT
	fakeCtaDstPort   = 3 // CTA_PROTO_DST_PORT

	fakeNetlinkPID = 4242
)

var errFakeWouldBlock = errors.New("fake netlink socket: no pending datagram (would block)")

// fakeKernelSocket is a netlink.Socket that models how the kernel frames
// ctnetlink replies: every message the kernel unicasts is its own skb and
// therefore its own datagram, and one recvmsg returns exactly one datagram.
//
// For a request, netlink_rcv_skb (net/netlink/af_netlink.c) runs the
// nfnetlink handler and then calls netlink_ack() if NLM_F_ACK is set or the
// handler returned an error. ctnetlink_get_conntrack unicasts the entry as a
// separate skb before that, so a successful GET with NLM_F_ACK yields two
// datagrams (entry, then ACK), while a failed GET/DELETE yields one error.
//
// nltest.Dial cannot be used here: its socket returns all replies to a
// request in one Receive call and discards unread replies on the next Send,
// which hides exactly the framing problem under test.
type fakeKernelSocket struct {
	entries   map[string]bool
	datagrams [][]netlink.Message
}

func newFakeKernelSocket() *fakeKernelSocket {
	return &fakeKernelSocket{entries: make(map[string]bool)}
}

func (s *fakeKernelSocket) Close() error { return nil }

func (s *fakeKernelSocket) SendMessages(msgs []netlink.Message) error {
	for _, m := range msgs {
		err := s.Send(m)
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *fakeKernelSocket) Send(req netlink.Message) error {
	key, err := fakeTupleKey(req.Data)
	if err != nil {
		return err
	}

	var errno fakeErrno
	switch int(req.Header.Type) & 0xff {
	case fakeCtMsgGet:
		if s.entries[key] {
			s.datagrams = append(s.datagrams, []netlink.Message{fakeEntryMessage(req, key)})
		} else {
			errno = fakeErrno(unix.ENOENT)
		}
	case fakeCtMsgDelete:
		if s.entries[key] {
			delete(s.entries, key)
		} else {
			errno = fakeErrno(unix.ENOENT)
		}
	default:
		errno = fakeErrno(unix.EOPNOTSUPP)
	}

	if req.Header.Flags&netlink.Acknowledge != 0 || errno != 0 {
		s.datagrams = append(s.datagrams, []netlink.Message{fakeAckMessage(req, errno)})
	}
	return nil
}

func (s *fakeKernelSocket) Receive() ([]netlink.Message, error) {
	if len(s.datagrams) == 0 {
		// A real socket would block here.
		return nil, errFakeWouldBlock
	}
	d := s.datagrams[0]
	s.datagrams = s.datagrams[1:]
	return d, nil
}

type fakeErrno int32

// fakeAckMessage builds an NLMSG_ERROR reply as built by netlink_ack().
func fakeAckMessage(req netlink.Message, errno fakeErrno) netlink.Message {
	data := make([]byte, 4+16)
	binary.NativeEndian.PutUint32(data[0:4], uint32(-int32(errno))) //nolint:gosec // Two's complement errno.
	binary.NativeEndian.PutUint32(data[4:8], req.Header.Length)
	binary.NativeEndian.PutUint16(data[8:10], uint16(req.Header.Type))
	binary.NativeEndian.PutUint16(data[10:12], uint16(req.Header.Flags))
	binary.NativeEndian.PutUint32(data[12:16], req.Header.Sequence)
	binary.NativeEndian.PutUint32(data[16:20], req.Header.PID)
	return netlink.Message{
		Header: netlink.Header{
			Type:     netlink.Error,
			Sequence: req.Header.Sequence,
			PID:      req.Header.PID,
		},
		Data: data,
	}
}

// fakeEntryMessage builds an IPCTNL_MSG_CT_NEW message describing the entry.
func fakeEntryMessage(req netlink.Message, key string) netlink.Message {
	parts := strings.Split(key, "|")
	src := net.ParseIP(parts[0]).To4()
	dst := net.ParseIP(parts[1]).To4()

	ip := netlink.NewAttributeEncoder()
	ip.Bytes(fakeCtaIPv4Src, src)
	ip.Bytes(fakeCtaIPv4Dst, dst)
	ipb, _ := ip.Encode()

	proto := netlink.NewAttributeEncoder()
	proto.ByteOrder = binary.BigEndian
	proto.Uint8(fakeCtaProtoNum, unix.IPPROTO_TCP)
	proto.Uint16(fakeCtaSrcPort, 40000)
	proto.Uint16(fakeCtaDstPort, 443)
	protob, _ := proto.Encode()

	tuple := netlink.NewAttributeEncoder()
	tuple.Bytes(unix.NLA_F_NESTED|fakeCtaTupleIP, ipb)
	tuple.Bytes(unix.NLA_F_NESTED|fakeCtaTupleProt, protob)
	tupleb, _ := tuple.Encode()

	top := netlink.NewAttributeEncoder()
	top.Bytes(unix.NLA_F_NESTED|fakeCtaTupleOrig, tupleb)
	topb, _ := top.Encode()

	return netlink.Message{
		Header: netlink.Header{
			Type:     netlink.HeaderType(unix.NFNL_SUBSYS_CTNETLINK<<8 | fakeCtMsgNew),
			Sequence: req.Header.Sequence,
			PID:      req.Header.PID,
		},
		Data: append([]byte{unix.AF_INET, unix.NFNETLINK_V0, 0, 0}, topb...),
	}
}

// fakeTupleKey extracts "src|dst" of the original tuple from a ctnetlink request.
func fakeTupleKey(data []byte) (string, error) {
	var src, dst net.IP
	top, err := netlink.NewAttributeDecoder(data[4:])
	if err != nil {
		return "", err
	}
	for top.Next() {
		if top.Type() != fakeCtaTupleOrig {
			continue
		}
		tuple, err := netlink.NewAttributeDecoder(top.Bytes())
		if err != nil {
			return "", err
		}
		for tuple.Next() {
			if tuple.Type() != fakeCtaTupleIP {
				continue
			}
			ip, err := netlink.NewAttributeDecoder(tuple.Bytes())
			if err != nil {
				return "", err
			}
			for ip.Next() {
				switch ip.Type() {
				case fakeCtaIPv4Src:
					src = net.IP(ip.Bytes())
				case fakeCtaIPv4Dst:
					dst = net.IP(ip.Bytes())
				}
			}
		}
	}
	return src.String() + "|" + dst.String(), nil
}

// newFakeNfct returns a go-conntrack handle whose netlink connection talks to
// a fakeKernelSocket. go-conntrack has no constructor accepting a connection,
// and Nfct's other fields are unexported, so a real handle is opened (this
// needs no privileges) and its exported Con is swapped for the fake.
func newFakeNfct(t *testing.T) (*ct.Nfct, *fakeKernelSocket) {
	t.Helper()

	h, err := ct.Open(&ct.Config{})
	if err != nil {
		t.Skipf("cannot open netfilter netlink socket: %s", err)
	}
	_ = h.Con.Close()

	sock := newFakeKernelSocket()
	h.Con = netlink.NewConn(sock, fakeNetlinkPID)
	t.Cleanup(func() { _ = h.Close() })
	return h, sock
}

func fakeCon(src, dst net.IP) ct.Con {
	proto := uint8(unix.IPPROTO_TCP)
	srcPort := uint16(40000)
	dstPort := uint16(443)
	return ct.Con{
		Origin: &ct.IPTuple{
			Src: &src,
			Dst: &dst,
			Proto: &ct.ProtoTuple{
				Number:  &proto,
				SrcPort: &srcPort,
				DstPort: &dstPort,
			},
		},
	}
}

// TestGoConntrackGetLeavesAckUnread documents the go-conntrack v0.4.0
// behaviour that caused the bug: Get sends NLM_F_ACK but reads only one
// datagram, so the ACK stays queued and the next request on the handle reads
// it and fails with a sequence mismatch. If this test starts failing after a
// go-conntrack upgrade, the library has been fixed.
func TestGoConntrackGetLeavesAckUnread(t *testing.T) {
	t.Parallel()

	h, sock := newFakeNfct(t)
	sock.entries["10.0.0.1|10.0.0.2"] = true
	sock.entries["10.0.0.3|10.0.0.4"] = true

	got, err := h.Get(ct.Conntrack, ct.IPv4, fakeCon(net.IPv4(10, 0, 0, 1), net.IPv4(10, 0, 0, 2)))
	if err != nil {
		t.Fatalf("Get: %s", err)
	}
	if len(got) != 1 {
		t.Fatalf("Get: expected 1 entry, got %d", len(got))
	}
	if len(sock.datagrams) != 1 {
		t.Fatalf("expected the ACK of Get to be left unread, %d datagrams pending", len(sock.datagrams))
	}

	// The next, unrelated request reads the stale ACK.
	err = h.Delete(ct.Conntrack, ct.IPv4, fakeCon(net.IPv4(10, 0, 0, 3), net.IPv4(10, 0, 0, 4)))
	if err == nil || !strings.Contains(err.Error(), "mismatched sequence") {
		t.Fatalf("Delete after Get: expected mismatched sequence error, got %v", err)
	}
	// The kernel did delete the entry, and its ACK is now the stale one.
	if sock.entries["10.0.0.3|10.0.0.4"] {
		t.Error("expected entry to be deleted by the fake kernel")
	}
	if len(sock.datagrams) != 1 {
		t.Errorf("expected the handle to stay desynchronized, %d datagrams pending", len(sock.datagrams))
	}
}

// TestDeleteMarkedConnectionFraming checks that DeleteMarkedConnection leaves
// no unread replies on the shared handle, so later requests still work.
//
//nolint:paralleltest // Modifies the package-level conntrack handle.
func TestDeleteMarkedConnectionFraming(t *testing.T) {
	h, sock := newFakeNfct(t)
	sock.entries["10.0.0.1|10.0.0.2"] = true
	sock.entries["10.0.0.3|10.0.0.4"] = true

	nfctLock.Lock()
	prev := nfct
	nfct = h
	nfctLock.Unlock()
	t.Cleanup(func() {
		nfctLock.Lock()
		nfct = prev
		nfctLock.Unlock()
	})

	// testConnection is 10.0.0.1:40000 -> 10.0.0.2:443/TCP.
	err := DeleteMarkedConnection(testConnection())
	if err != nil {
		t.Fatalf("DeleteMarkedConnection: %s", err)
	}
	if sock.entries["10.0.0.1|10.0.0.2"] {
		t.Error("DeleteMarkedConnection: entry was not deleted")
	}
	if len(sock.datagrams) != 0 {
		t.Fatalf("DeleteMarkedConnection left %d unread datagrams on the handle", len(sock.datagrams))
	}

	// A missing entry is reported as an error and leaves nothing behind.
	err = DeleteMarkedConnection(testConnection())
	if !errors.Is(err, unix.ENOENT) {
		t.Errorf("DeleteMarkedConnection on missing entry: expected ENOENT, got %v", err)
	}
	if len(sock.datagrams) != 0 {
		t.Fatalf("DeleteMarkedConnection (missing) left %d unread datagrams", len(sock.datagrams))
	}

	// The next request on the shared handle must still be in sync.
	err = h.Delete(ct.Conntrack, ct.IPv4, fakeCon(net.IPv4(10, 0, 0, 3), net.IPv4(10, 0, 0, 4)))
	if err != nil {
		t.Fatalf("Delete after DeleteMarkedConnection: %s", err)
	}
}
