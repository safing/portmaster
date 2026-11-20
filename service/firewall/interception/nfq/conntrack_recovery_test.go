//go:build linux

package nfq

import (
	"errors"
	"fmt"
	"net"
	"os"
	"sync"
	"syscall"
	"testing"

	ct "github.com/florianl/go-conntrack"
	"github.com/mdlayher/netlink"
)

var (
	errFakeMismatchedSeq = errors.New("mismatched sequence in netlink reply")
	errFakeNoHandleLeft  = errors.New("fake open: no handle left")
	errFakeOpenFailed    = errors.New("fake open: failed")
	errFakeOther         = errors.New("other error")
)

// Errors as returned by github.com/mdlayher/netlink v1.8.0.
var (
	// recvmsg failed with ENOBUFS: the socket receive buffer overran.
	errFakeENOBUFS = &netlink.OpError{Op: netlinkOpReceive, Err: os.NewSyscallError("recvmsg", syscall.ENOBUFS)}
	// Conn.Execute read a reply of an earlier request.
	errFakeSeqMismatch = &netlink.OpError{Op: netlinkOpValidate, Err: errFakeMismatchedSeq}
	// The kernel answered with an NLMSG_ERROR carrying ENOENT.
	errFakeENOENT = &netlink.OpError{Op: netlinkOpReceive, Err: syscall.ENOENT}
)

// fakeHandle is a conntrackHandle with scripted errors.
type fakeHandle struct {
	mu        sync.Mutex
	queryErr  error // returned by every Query, if set
	deleteErr error // returned by every Delete, if set
	entries   []ct.Con
	deleted   []ct.Con // attributes of every Delete call
	queries   int
	deletes   int
	closed    bool
}

func (h *fakeHandle) Query(_ ct.Table, _ ct.Family, _ ct.FilterAttr) ([]ct.Con, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.queries++
	if h.queryErr != nil {
		return nil, h.queryErr
	}
	return append([]ct.Con(nil), h.entries...), nil
}

func (h *fakeHandle) Delete(_ ct.Table, _ ct.Family, attributes ct.Con) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.deletes++
	h.deleted = append(h.deleted, attributes)
	if h.deleteErr != nil {
		return h.deleteErr
	}
	if len(h.entries) == 0 {
		return errFakeENOENT
	}
	h.entries = h.entries[1:]
	return nil
}

func (h *fakeHandle) Close() error {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.closed = true
	return nil
}

func (h *fakeHandle) isClosed() bool {
	h.mu.Lock()
	defer h.mu.Unlock()

	return h.closed
}

// fakeOpener replaces openConntrack and hands out the given handles in order.
// A nil handle makes the open fail.
type fakeOpener struct {
	mu      sync.Mutex
	handles []conntrackHandle
	opens   int
}

func (o *fakeOpener) open() (conntrackHandle, error) {
	o.mu.Lock()
	defer o.mu.Unlock()

	o.opens++
	if len(o.handles) == 0 {
		return nil, errFakeNoHandleLeft
	}
	h := o.handles[0]
	o.handles = o.handles[1:]
	if h == nil {
		return nil, errFakeOpenFailed
	}
	return h, nil
}

func (o *fakeOpener) count() int {
	o.mu.Lock()
	defer o.mu.Unlock()

	return o.opens
}

// installFakeConntrack sets current as the package handle and replaces
// openConntrack with a fakeOpener handing out next. Everything is restored
// when the test ends.
func installFakeConntrack(t *testing.T, current conntrackHandle, next ...conntrackHandle) *fakeOpener {
	t.Helper()

	opener := &fakeOpener{handles: next}

	nfctLock.Lock()
	prevNfct, prevReopen, prevOpen := nfct, nfctReopen, openConntrack
	nfct, nfctReopen, openConntrack = current, false, opener.open
	nfctLock.Unlock()

	t.Cleanup(func() {
		nfctLock.Lock()
		nfct, nfctReopen, openConntrack = prevNfct, prevReopen, prevOpen
		nfctLock.Unlock()
	})
	return opener
}

func testEntry() ct.Con {
	return fakeCon(net.IPv4(10, 0, 0, 1), net.IPv4(10, 0, 0, 2))
}

// TestIsDesyncError checks the classification against errors produced by the
// real go-conntrack/netlink code paths.
func TestIsDesyncError(t *testing.T) {
	t.Parallel()

	// Kernel error reply: benign, handle stays in sync.
	h, _ := newFakeNfct(t)
	err := h.Delete(ct.Conntrack, ct.IPv4, testEntry())
	if !errors.Is(err, syscall.ENOENT) {
		t.Fatalf("Delete of missing entry: expected ENOENT, got %v", err)
	}
	if isDesyncError(err) {
		t.Errorf("ENOENT must not be a desync error: %v", err)
	}

	// Stale reply of an earlier request: sequence mismatch.
	h, sock := newFakeNfct(t)
	sock.datagrams = append(sock.datagrams, []netlink.Message{fakeAckMessage(netlink.Message{
		Header: netlink.Header{Sequence: 0xdead, PID: fakeNetlinkPID},
	}, 0)})
	err = h.Delete(ct.Conntrack, ct.IPv4, testEntry())
	if err == nil {
		t.Fatal("Delete after stale reply: expected error")
	}
	if !isDesyncError(err) {
		t.Errorf("sequence mismatch must be a desync error: %v", err)
	}

	// Socket receive error (ENOBUFS) during Delete and Query.
	h = newNfctWithSocket(t, &failingSocket{fakeKernelSocket: newFakeKernelSocket(), recvErr: syscall.ENOBUFS})
	err = h.Delete(ct.Conntrack, ct.IPv4, testEntry())
	if !isDesyncError(err) {
		t.Errorf("Delete with ENOBUFS on recvmsg must be a desync error: %v", err)
	}
	_, err = h.Query(ct.Conntrack, ct.IPv4, ct.FilterAttr{
		Mark:     []byte{0, 0, 0, 0},
		MarkMask: []byte{0xFF, 0xFF, 0xFF, 0xFF},
	})
	if !isDesyncError(err) {
		t.Errorf("Query with ENOBUFS on recvmsg must be a desync error: %v", err)
	}

	// go-conntrack fails to encode the request before sending anything.
	_, err = h.Query(ct.Conntrack, ct.IPv4, ct.FilterAttr{})
	if err == nil || isDesyncError(err) {
		t.Errorf("request encoding error must not be a desync error: %v", err)
	}

	// Not a netlink error at all, and wrapped errors.
	if isDesyncError(nil) || isDesyncError(errFakeOther) {
		t.Error("nil and unrelated errors must not be desync errors")
	}
	if !isDesyncError(fmt.Errorf("wrapped: %w", errFakeSeqMismatch)) {
		t.Error("wrapped sequence mismatch must be a desync error")
	}
}

// failingSocket fails every Receive with recvErr as recvmsg would.
type failingSocket struct {
	*fakeKernelSocket

	recvErr error
}

func (s *failingSocket) Receive() ([]netlink.Message, error) {
	return nil, os.NewSyscallError("recvmsg", s.recvErr)
}

func newNfctWithSocket(t *testing.T, sock *failingSocket) *ct.Nfct {
	t.Helper()

	h, err := ct.Open(&ct.Config{})
	if err != nil {
		t.Skipf("cannot open netfilter netlink socket: %s", err)
	}
	_ = h.Con.Close()
	h.Con = netlink.NewConn(sock, fakeNetlinkPID)
	t.Cleanup(func() { _ = h.Close() })
	return h
}

// TestDeleteMarkedConnectionRecoversFromDesync uses the real go-conntrack
// handle on a fake kernel socket that has a stale reply queued (as left by
// the old Get+Delete code). Without recovery, every later request reads the
// reply of the previous one and fails forever.
//
//nolint:paralleltest // Modifies the package-level conntrack handle.
func TestDeleteMarkedConnectionRecoversFromDesync(t *testing.T) {
	desynced, desyncedSock := newFakeNfct(t)
	desyncedSock.entries["10.0.0.1|10.0.0.2"] = true
	desyncedSock.datagrams = append(desyncedSock.datagrams, []netlink.Message{fakeAckMessage(netlink.Message{
		Header: netlink.Header{Sequence: 0xdead, PID: fakeNetlinkPID},
	}, 0)})

	fresh, freshSock := newFakeNfct(t)
	freshSock.entries["10.0.0.1|10.0.0.2"] = true

	opener := installFakeConntrack(t, desynced, fresh)

	// testConnection is 10.0.0.1:40000 -> 10.0.0.2:443/TCP.
	err := DeleteMarkedConnection(testConnection())
	if err != nil {
		t.Errorf("first DeleteMarkedConnection after desync: %s", err)
	}
	err = DeleteMarkedConnection(testConnection())
	if !errors.Is(err, syscall.ENOENT) {
		t.Errorf("second DeleteMarkedConnection: expected ENOENT (entry already deleted), got %v", err)
	}
	if n := opener.count(); n != 1 {
		t.Errorf("expected the handle to be reopened exactly once, got %d opens", n)
	}
	if len(freshSock.datagrams) != 0 {
		t.Errorf("reopened handle has %d unread datagrams", len(freshSock.datagrams))
	}
}

// TestDeleteAllMarkedConnectionRecoversFromDesync checks that a dump that
// fails with ENOBUFS is restarted on a reopened handle.
//
//nolint:paralleltest // Modifies the package-level conntrack handle.
func TestDeleteAllMarkedConnectionRecoversFromDesync(t *testing.T) {
	broken := &fakeHandle{queryErr: errFakeENOBUFS, deleteErr: errFakeSeqMismatch}
	fresh := &fakeHandle{entries: []ct.Con{testEntry(), testEntry()}}
	opener := installFakeConntrack(t, broken, fresh)

	err := DeleteAllMarkedConnection()
	if err != nil {
		t.Fatalf("DeleteAllMarkedConnection: %s", err)
	}
	if !broken.isClosed() {
		t.Error("desynchronized handle was not closed")
	}
	if n := opener.count(); n != 1 {
		t.Errorf("expected exactly one reopen, got %d", n)
	}
	if broken.queries != 1 {
		t.Errorf("expected the broken handle to be used once, got %d queries", broken.queries)
	}
	if fresh.deletes != 2 || len(fresh.entries) != 0 {
		t.Errorf("expected both entries to be deleted on the reopened handle, got %d deletes, %d left",
			fresh.deletes, len(fresh.entries))
	}
}

// TestConntrackBenignErrorKeepsHandle checks that a kernel error reply such as
// ENOENT does not reopen the handle.
//
//nolint:paralleltest // Modifies the package-level conntrack handle.
func TestConntrackBenignErrorKeepsHandle(t *testing.T) {
	h := &fakeHandle{}
	opener := installFakeConntrack(t, h)

	err := DeleteMarkedConnection(testConnection())
	if !errors.Is(err, syscall.ENOENT) {
		t.Fatalf("expected ENOENT, got %v", err)
	}
	if h.isClosed() || opener.count() != 0 || h.deletes != 1 {
		t.Errorf("benign error must not reopen or retry: closed=%v opens=%d deletes=%d",
			h.isClosed(), opener.count(), h.deletes)
	}
}

// TestConntrackRetriesOnce checks that a failed operation is retried at most
// once, and that a handle that is still out of sync after the retry is
// replaced on the next call.
//
//nolint:paralleltest // Modifies the package-level conntrack handle.
func TestConntrackRetriesOnce(t *testing.T) {
	first := &fakeHandle{deleteErr: errFakeSeqMismatch}
	second := &fakeHandle{deleteErr: errFakeSeqMismatch}
	third := &fakeHandle{entries: []ct.Con{testEntry()}}
	opener := installFakeConntrack(t, first, second, third)

	err := DeleteMarkedConnection(testConnection())
	if err == nil {
		t.Fatal("expected error when the retry fails as well")
	}
	if first.deletes != 1 || second.deletes != 1 || opener.count() != 1 {
		t.Errorf("expected one attempt per handle and one reopen: %d, %d, opens=%d",
			first.deletes, second.deletes, opener.count())
	}
	if !first.isClosed() || !second.isClosed() {
		t.Error("desynchronized handles were not closed")
	}

	err = DeleteMarkedConnection(testConnection())
	if err != nil {
		t.Errorf("next call on a reopened handle: %s", err)
	}
	if opener.count() != 2 || third.deletes != 1 {
		t.Errorf("expected the next call to reopen: opens=%d deletes=%d", opener.count(), third.deletes)
	}
}

// TestConntrackReopenFailure checks that a failed reopen leaves a state from
// which a later call recovers, and that teardown still wins.
//
//nolint:paralleltest // Modifies the package-level conntrack handle.
func TestInitNFCTClosesPreviousHandle(t *testing.T) {
	old := &fakeHandle{}
	fresh := &fakeHandle{}
	opener := installFakeConntrack(t, old, fresh)

	err := InitNFCT()
	if err != nil {
		t.Fatalf("InitNFCT: %s", err)
	}
	if !old.isClosed() {
		t.Error("InitNFCT did not close the previously open handle")
	}
	if fresh.isClosed() {
		t.Error("InitNFCT closed the new handle")
	}
	if n := opener.count(); n != 1 {
		t.Errorf("expected 1 open, got %d", n)
	}

	TeardownNFCT()
	if !fresh.isClosed() {
		t.Error("TeardownNFCT did not close the new handle")
	}
}

//nolint:paralleltest // Modifies the package-level conntrack handle.
func TestConntrackReopenFailure(t *testing.T) {
	broken := &fakeHandle{deleteErr: errFakeENOBUFS}
	fresh := &fakeHandle{entries: []ct.Con{testEntry()}}
	opener := installFakeConntrack(t, broken, nil, nil, fresh)

	err := DeleteMarkedConnection(testConnection())
	if !errors.Is(err, syscall.ENOBUFS) {
		t.Fatalf("expected the original ENOBUFS error, got %v", err)
	}
	err = DeleteAllMarkedConnection()
	if err == nil {
		t.Error("DeleteAllMarkedConnection: expected error while the handle cannot be reopened")
	}
	err = DeleteMarkedConnection(testConnection())
	if err != nil {
		t.Errorf("DeleteMarkedConnection after the open works again: %s", err)
	}
	if n := opener.count(); n != 3 {
		t.Errorf("expected 3 open attempts, got %d", n)
	}

	TeardownNFCT()
	if !fresh.isClosed() {
		t.Error("TeardownNFCT did not close the reopened handle")
	}
	err = DeleteMarkedConnection(testConnection())
	if !errors.Is(err, errNFCTNotInitialized) {
		t.Errorf("after teardown: expected not initialized, got %v", err)
	}
	if n := opener.count(); n != 3 {
		t.Errorf("handle must not be reopened after teardown, got %d opens", n)
	}
}

// TestConntrackRetriedDeleteENOENT checks that ENOENT on the retry of a
// delete is not reported as a failure: the kernel may have executed the first
// attempt before its reply was lost or left behind, so the entry is gone
// either way. ENOENT on a first attempt is still reported (see
// TestConntrackBenignErrorKeepsHandle).
//
//nolint:paralleltest // Modifies the package-level conntrack handle.
func TestConntrackRetriedDeleteENOENT(t *testing.T) {
	// The kernel deleted the entry, but its ACK was lost (ENOBUFS); the
	// entry is no longer in the table when the delete is retried.
	broken := &fakeHandle{deleteErr: errFakeENOBUFS}
	fresh := &fakeHandle{}
	opener := installFakeConntrack(t, broken, fresh)

	err := DeleteMarkedConnection(testConnection())
	if err != nil {
		t.Errorf("retried delete that found no entry reported a failure: %s", err)
	}
	if opener.count() != 1 || broken.deletes != 1 || fresh.deletes != 1 {
		t.Errorf("expected one reopen and one retry: opens=%d deletes=%d,%d",
			opener.count(), broken.deletes, fresh.deletes)
	}
	// ENOENT on the first attempt of the next delete is still an error.
	err = DeleteMarkedConnection(testConnection())
	if !errors.Is(err, syscall.ENOENT) {
		t.Errorf("first-attempt ENOENT: expected ENOENT, got %v", err)
	}

	// The same applies to the deletes of DeleteAllMarkedConnection: the
	// queried entry counts as deleted. Later queries go to the empty
	// reopened handle.
	broken2 := &fakeHandle{entries: []ct.Con{testEntry()}, deleteErr: errFakeSeqMismatch}
	installFakeConntrack(t, broken2, &fakeHandle{})
	nfctLock.Lock()
	deleted := deleteMarkedConnections(ct.IPv4)
	nfctLock.Unlock()
	if deleted != 1 {
		t.Errorf("expected 1 deleted entry, got %d", deleted)
	}
}
