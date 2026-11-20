//go:build linux

package nfq

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"sync"
	"syscall"

	ct "github.com/florianl/go-conntrack"
	"github.com/mdlayher/netlink"

	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/service/netenv"
	"github.com/safing/portmaster/service/network"
	pmpacket "github.com/safing/portmaster/service/network/packet"
)

// conntrackHandle is the part of *ct.Nfct used by this package. It allows
// tests to substitute a fake handle.
type conntrackHandle interface {
	Query(t ct.Table, f ct.Family, filter ct.FilterAttr) ([]ct.Con, error)
	Delete(t ct.Table, f ct.Family, attributes ct.Con) error
	Close() error
}

// openConntrack opens a new conntrack handle. It is replaced in tests.
var openConntrack = func() (conntrackHandle, error) {
	h, err := ct.Open(&ct.Config{})
	if err != nil {
		// Do not return a typed nil pointer as a non-nil interface.
		return nil, err
	}
	return h, nil
}

var (
	errNFCTNotInitialized   = errors.New("nfq: nfct not initialized")
	errConnectionIncomplete = errors.New("local or remote address unknown")
)

var (
	// nfct is the conntrack handler. NFCT: Network Filter Connection Tracking.
	// It is nil before InitNFCT, after TeardownNFCT, and after it was closed
	// because it desynchronized and could not be reopened yet (see nfctReopen).
	nfct conntrackHandle
	// nfctReopen is set when nfct was closed because it desynchronized. The
	// next operation tries to reopen it.
	nfctReopen bool
	// nfctLock guards nfct and nfctReopen. It is held for the whole of every
	// exported function in this file, so that each request/reply exchange on
	// the handle (including complete query+delete loops) is exclusive and
	// Close, reopen and reassignment never overlap a request in flight.
	// The handle is not safe for concurrent use: go-conntrack's Query sends
	// the request and reads the reply in two separately locked netlink calls
	// (Conn.Send, then Conn.Receive) without matching the reply to the
	// request, so concurrent callers could receive each other's replies.
	// nfctLock is a leaf lock: nothing called while holding it may acquire
	// other Portmaster locks (callers hold connection locks).
	nfctLock sync.Mutex
)

// InitNFCT initializes the network filter conntrack library.
func InitNFCT() error {
	nfctLock.Lock()
	defer nfctLock.Unlock()

	// Do not leak the netlink socket of a handle that is still open.
	if nfct != nil {
		_ = nfct.Close()
		nfct = nil
	}
	nfctReopen = false
	h, err := openConntrack()
	if err != nil {
		return err
	}
	nfct = h
	return nil
}

// TeardownNFCT deinitializes the network filter conntrack library.
func TeardownNFCT() {
	nfctLock.Lock()
	defer nfctLock.Unlock()

	if nfct != nil {
		_ = nfct.Close()
		nfct = nil
	}
	nfctReopen = false
}

// nfctHandleLocked returns the conntrack handle, reopening it first if it was
// closed because it desynchronized. nfctLock must be held.
func nfctHandleLocked() (conntrackHandle, error) {
	if nfct != nil {
		return nfct, nil
	}
	if !nfctReopen {
		return nil, errNFCTNotInitialized
	}

	h, err := openConntrack()
	if err != nil {
		return nil, fmt.Errorf("nfq: failed to reopen conntrack handle: %w", err)
	}
	nfct = h
	nfctReopen = false
	log.Infof("nfq: reopened conntrack handle")
	return h, nil
}

// invalidateNFCTLocked closes the desynchronized conntrack handle, so that the
// next operation opens a new one. nfctLock must be held.
func invalidateNFCTLocked(cause error) {
	log.Warningf("nfq: conntrack handle is out of sync, reopening it: %s", cause)
	if nfct != nil {
		_ = nfct.Close()
		nfct = nil
	}
	nfctReopen = true
}

// Op values of *netlink.OpError (github.com/mdlayher/netlink v1.8.0).
const (
	netlinkOpReceive  = "receive"
	netlinkOpValidate = "validate"
)

// isDesyncError reports whether err from a request on the conntrack handle
// means that replies may be left unread on (or lost from) its socket, so that
// later requests would read replies that do not belong to them.
//
// Errors of github.com/mdlayher/netlink v1.8.0 (used by go-conntrack v0.4.0):
//   - Op "validate": Conn.Execute (used by Delete) read a reply with another
//     sequence number or PID, i.e. a stale reply of an earlier request, and
//     the reply to this request is still queued.
//   - Op "receive" with a bare syscall.Errno: the kernel answered with an
//     NLMSG_ERROR (e.g. ENOENT for a missing entry). It is the last message of
//     the reply, so the handle is still in sync. A truncated datagram
//     (syscall.ParseNetlinkMessage) would also yield a bare EINVAL, but
//     Receive sizes its buffer with MSG_PEEK first, so that cannot happen
//     with a single reader.
//   - Op "receive" with any other error (an *os.SyscallError from recvmsg,
//     e.g. ENOBUFS after the receive buffer overran and the kernel dropped
//     replies, or a malformed error message): the read was aborted, possibly
//     in the middle of a multi-part dump reply.
//   - Op "send": the request did not reach the kernel, no reply is queued.
//
// ENOBUFS is always treated as a desync, also when the kernel reports it in
// an error message, as replies may have been dropped.
// Errors that are not a *netlink.OpError come from go-conntrack itself while
// encoding a request (before sending) or decoding a reply that was read
// completely; they do not affect the socket.
func isDesyncError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, syscall.ENOBUFS) {
		return true
	}

	var opErr *netlink.OpError
	if !errors.As(err, &opErr) {
		return false
	}
	switch opErr.Op {
	case netlinkOpValidate:
		return true
	case netlinkOpReceive:
		// The exact type distinguishes kernel error replies from socket errors.
		_, kernelErrno := opErr.Err.(syscall.Errno) //nolint:errorlint
		return !kernelErrno
	default:
		return false
	}
}

// withNFCTLocked runs op on the conntrack handle. If op fails because the
// handle desynchronized, the handle is closed and reopened and op is retried
// once. op must be safe to retry. nfctLock must be held.
func withNFCTLocked(op func(h conntrackHandle) error) error {
	h, err := nfctHandleLocked()
	if err != nil {
		return err
	}

	err = op(h)
	if !isDesyncError(err) {
		return err
	}

	invalidateNFCTLocked(err)
	h, reopenErr := nfctHandleLocked()
	if reopenErr != nil {
		return errors.Join(err, reopenErr)
	}
	err = op(h)
	if isDesyncError(err) {
		invalidateNFCTLocked(err)
	}
	return err
}

// queryLocked runs a conntrack dump query. A dump can be restarted, so it is
// retried once on a desynchronized handle. nfctLock must be held.
func queryLocked(f ct.Family, filter ct.FilterAttr) ([]ct.Con, error) {
	var connections []ct.Con
	err := withNFCTLocked(func(h conntrackHandle) error {
		var err error
		connections, err = h.Query(ct.Conntrack, f, filter)
		return err
	})
	return connections, err
}

// deleteLocked deletes a conntrack entry. Deleting is idempotent, so it is
// retried once on a desynchronized handle. nfctLock must be held.
//
// The kernel may have executed the first attempt before its reply was lost
// (ENOBUFS) or left unread (mismatched sequence), so ENOENT on the retry
// cannot be told apart from an entry that did not exist. Either way the entry
// is gone, so ENOENT on the retry counts as success. ENOENT on the first
// attempt is still returned.
func deleteLocked(f ct.Family, connection ct.Con) error {
	retry := false
	return withNFCTLocked(func(h conntrackHandle) error {
		err := h.Delete(ct.Conntrack, f, connection)
		if retry && errors.Is(err, syscall.ENOENT) {
			return nil
		}
		retry = true
		return err
	})
}

// DeleteUnmarkedConnections deletes all conntrack entries with connmark=0,
// excluding loopback connections.
// These entries represent connections established while Portmaster was not
// running or was paused and therefore never received a verdict mark.
//
// The Linux netfilter nat table applies DNAT only to the first packet of a NEW
// connection. ESTABLISHED connections bypass the nat table entirely, so any
// routing decision (e.g. MarkRerouteSPN, MarkRerouteSplitTun) would never take
// effect for them. Removing their conntrack entries forces applications to
// reconnect; the resulting SYN is processed by NFQUEUE as a new connection and
// the correct DNAT rule fires.
//
// Loopback connections (source or destination is 127.x.x.x / ::1) are skipped.
// They always carry connmark=0 because Portmaster never saves a permanent mark
// for loopback-destined packets. Flushing them would needlessly disconnect apps
// talking to local services (databases, dev servers, local APIs, etc.).
//
// Connections already processed by Portmaster carry a non-zero connmark and
// are handled via CONNMARK --restore-mark; they are unaffected.
func DeleteUnmarkedConnections() error {
	nfctLock.Lock()
	defer nfctLock.Unlock()

	_, err := nfctHandleLocked()
	if err != nil {
		return err
	}

	deleted := deleteUnmarkedConnections(ct.IPv4)

	if netenv.IPv6Enabled() {
		deleted += deleteUnmarkedConnections(ct.IPv6)
	}

	log.Infof("nfq: deleted %d unmarked conntrack entries to force re-evaluation on firewall activation", deleted)
	return nil
}

func deleteUnmarkedConnections(f ct.Family) (deleted int) {
	filter := ct.FilterAttr{
		Mark:     []byte{0x00, 0x00, 0x00, 0x00},
		MarkMask: []byte{0xFF, 0xFF, 0xFF, 0xFF},
	}

	connections, err := queryLocked(f, filter)
	if err != nil {
		log.Warningf("nfq: error querying unmarked conntrack entries: %s", err)
		return 0
	}

	var lastErr error
	for _, connection := range connections {
		if isLoopbackConnection(connection) {
			continue
		}
		err := deleteLocked(f, connection)
		if err != nil {
			lastErr = err
		} else {
			deleted++
		}
	}

	if lastErr != nil {
		log.Warningf("nfq: some unmarked conntrack entries could not be deleted, last error: %s", lastErr)
	}
	return deleted
}

// isLoopbackConnection reports whether a conntrack entry involves a loopback address.
func isLoopbackConnection(c ct.Con) bool {
	if c.Origin != nil {
		if c.Origin.Src != nil && c.Origin.Src.IsLoopback() {
			return true
		}
		if c.Origin.Dst != nil && c.Origin.Dst.IsLoopback() {
			return true
		}
	}
	if c.Reply != nil {
		if c.Reply.Src != nil && c.Reply.Src.IsLoopback() {
			return true
		}
		if c.Reply.Dst != nil && c.Reply.Dst.IsLoopback() {
			return true
		}
	}
	return false
}

// DeleteAllMarkedConnection deletes all marked entries from the conntrack table.
func DeleteAllMarkedConnection() error {
	nfctLock.Lock()
	defer nfctLock.Unlock()

	_, err := nfctHandleLocked()
	if err != nil {
		return err
	}

	// Delete all ipv4 marked connections
	deleted := deleteMarkedConnections(ct.IPv4)

	if netenv.IPv6Enabled() {
		// Delete all ipv6 marked connections
		deleted += deleteMarkedConnections(ct.IPv6)
	}

	log.Infof("nfq: deleted %d conntrack entries to reset permanent connection verdicts", deleted)
	return nil
}

func deleteMarkedConnections(f ct.Family) (deleted int) {
	// initialize variables
	permanentFlags := []uint32{MarkAcceptAlways, MarkBlockAlways, MarkDropAlways, MarkRerouteNS, MarkRerouteSPN, MarkRerouteSplitTun}
	filter := ct.FilterAttr{}
	filter.MarkMask = []byte{0xFF, 0xFF, 0xFF, 0xFF}
	filter.Mark = []byte{0x00, 0x00, 0x00, 0x00} // 4 zeros starting value

	numberOfErrors := 0
	var deleteError error = nil
	// Get all connections from the specified family (ipv4 or ipv6)
	for _, mark := range permanentFlags {
		binary.BigEndian.PutUint32(filter.Mark, mark) // Little endian is in reverse not sure why. BigEndian makes it in correct order.
		currentConnections, err := queryLocked(f, filter)
		if err != nil {
			log.Warningf("nfq: error on conntrack query: %s", err)
			continue
		}

		for _, connection := range currentConnections {
			deleteError = deleteLocked(f, connection)
			if deleteError != nil {
				numberOfErrors++
			} else {
				deleted++
			}
		}
	}

	if numberOfErrors > 0 {
		log.Warningf("nfq: failed to delete %d conntrack entries last error is: %s", numberOfErrors, deleteError)
	}
	return deleted
}

// DeleteMarkedConnection removes a specific connection from the conntrack table.
func DeleteMarkedConnection(conn *network.Connection) error {
	// Incomplete connections have no remote entity (and no local address) yet.
	if conn.Entity == nil || conn.Entity.IP == nil || conn.LocalIP == nil {
		return fmt.Errorf("nfq: cannot delete conntrack entry for connection %s: %w", conn.ID, errConnectionIncomplete)
	}

	nfctLock.Lock()
	defer nfctLock.Unlock()

	_, err := nfctHandleLocked()
	if err != nil {
		return err
	}

	con := ct.Con{
		Origin: &ct.IPTuple{
			Src: &conn.LocalIP,
			Dst: &conn.Entity.IP,
			Proto: &ct.ProtoTuple{
				Number:  &conn.Entity.Protocol,
				SrcPort: &conn.LocalPort,
				DstPort: &conn.Entity.Port,
			},
		},
	}

	family := ct.IPv4
	if conn.IPVersion == pmpacket.IPv6 {
		family = ct.IPv6
	}

	// Delete by original tuple directly instead of Get followed by Delete:
	// go-conntrack v0.4.0 sends Get with NLM_F_ACK but reads only the entry
	// message, leaving the kernel's ACK queued on the shared socket. The next
	// request on the handle then read that stale ACK ("mismatched sequence")
	// and left its own reply behind, desynchronizing the handle for all later
	// requests. The kernel answers a Delete with exactly one message (ACK or
	// error), which Delete reads. A missing entry is reported as ENOENT.
	err = deleteLocked(family, con)
	if err != nil {
		return fmt.Errorf("nfq: failed to delete conntrack entry for connection %s: %w", conn.String(), err)
	}

	return nil
}

// DeleteFlow removes the conntrack entry whose original direction matches the
// given addresses, if there is one, and reports whether it did. A missing
// entry is not an error.
func DeleteFlow(protocol uint8, localIP net.IP, localPort uint16, remoteIP net.IP, remotePort uint16) (deleted bool, err error) {
	// Go often holds IPv4 addresses in 16 byte form, which conntrack would
	// not match against an IPv4 entry.
	family := ct.IPv4
	if ip4 := localIP.To4(); ip4 != nil {
		localIP = ip4
		remoteIP = remoteIP.To4()
	} else {
		family = ct.IPv6
	}
	if remoteIP == nil {
		return false, fmt.Errorf("nfq: %w: mixed IP versions", errConnectionIncomplete)
	}

	nfctLock.Lock()
	defer nfctLock.Unlock()

	_, err = nfctHandleLocked()
	if err != nil {
		return false, err
	}

	err = deleteLocked(family, ct.Con{
		Origin: &ct.IPTuple{
			Src: &localIP,
			Dst: &remoteIP,
			Proto: &ct.ProtoTuple{
				Number:  &protocol,
				SrcPort: &localPort,
				DstPort: &remotePort,
			},
		},
	})
	switch {
	case errors.Is(err, syscall.ENOENT):
		return false, nil
	case err != nil:
		return false, err
	default:
		return true, nil
	}
}
