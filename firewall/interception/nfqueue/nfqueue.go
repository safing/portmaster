// +build linux

package nfqueue

/*
#cgo LDFLAGS: -lnetfilter_queue
#cgo CFLAGS: -Wall
#include "nfqueue.h"
*/
import "C"

import (
	"errors"
	"fmt"
	"net"
	"os"
	"runtime"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/safing/portmaster/network/packet"
)

var queues map[uint16]*NFQueue

func init() {
	queues = make(map[uint16]*NFQueue)
}

// NFQueue holds a Linux NFQ Handle and associated information.
//nolint:maligned // FIXME
type NFQueue struct {
	DefaultVerdict uint32
	Timeout        time.Duration
	qid            uint16
	qidptr         *uint16
	h              *C.struct_nfq_handle
	//qh             *C.struct_q_handle
	qh *C.struct_nfq_q_handle
	fd int
	lk sync.Mutex

	Packets chan packet.Packet
}

// NewNFQueue initializes a new netfilter queue.
func NewNFQueue(qid uint16) (nfq *NFQueue, err error) {
	if os.Geteuid() != 0 {
		return nil, errors.New("must be root to intercept packets")
	}
	nfq = &NFQueue{DefaultVerdict: NFQ_DROP, Timeout: 3000 * time.Millisecond, qid: qid, qidptr: &qid}
	queues[nfq.qid] = nfq

	err = nfq.init()
	if err != nil {
		return nil, err
	}

	go func() {
		runtime.LockOSThread()
		C.loop_for_packets(nfq.h)
	}()

	return nfq, nil
}

func (nfq *NFQueue) init() error {
	var err error
	if nfq.h, err = C.nfq_open(); err != nil || nfq.h == nil {
		return fmt.Errorf("could not open nfqueue: %s", err)
	}

	//if nfq.qh, err = C.nfq_create_queue(nfq.h, qid, C.get_cb(), unsafe.Pointer(nfq)); err != nil || nfq.qh == nil {

	nfq.Packets = make(chan packet.Packet, 1)

	if C.nfq_unbind_pf(nfq.h, C.AF_INET) < 0 {
		nfq.Destroy()
		return errors.New("nfq_unbind_pf(AF_INET) failed, are you root?")
	}
	if C.nfq_unbind_pf(nfq.h, C.AF_INET6) < 0 {
		nfq.Destroy()
		return errors.New("nfq_unbind_pf(AF_INET6) failed")
	}

	if C.nfq_bind_pf(nfq.h, C.AF_INET) < 0 {
		nfq.Destroy()
		return errors.New("nfq_bind_pf(AF_INET) failed")
	}
	if C.nfq_bind_pf(nfq.h, C.AF_INET6) < 0 {
		nfq.Destroy()
		return errors.New("nfq_bind_pf(AF_INET6) failed")
	}

	if nfq.qh, err = C.create_queue(nfq.h, C.uint16_t(nfq.qid)); err != nil || nfq.qh == nil {
		C.nfq_close(nfq.h)
		return fmt.Errorf("could not create queue: %s", err)
	}

	nfq.fd = int(C.nfq_fd(nfq.h))

	if C.nfq_set_mode(nfq.qh, C.NFQNL_COPY_PACKET, 0xffff) < 0 {
		nfq.Destroy()
		return errors.New("nfq_set_mode(NFQNL_COPY_PACKET) failed")
	}
	if C.nfq_set_queue_maxlen(nfq.qh, 1024*8) < 0 {
		nfq.Destroy()
		return errors.New("nfq_set_queue_maxlen(1024 * 8) failed")
	}

	return nil
}

// Destroy closes all the nfqueues.
func (nfq *NFQueue) Destroy() {
	nfq.lk.Lock()
	defer nfq.lk.Unlock()

	if nfq.fd != 0 && nfq.Valid() {
		syscall.Close(nfq.fd)
	}
	if nfq.qh != nil {
		C.nfq_destroy_queue(nfq.qh)
		nfq.qh = nil
	}
	if nfq.h != nil {
		C.nfq_close(nfq.h)
		nfq.h = nil
	}

	// TODO: don't close, we're exiting anyway
	// if nfq.Packets != nil {
	// 	close(nfq.Packets)
	// }
}

// Valid returns whether the NFQueue is still valid.
func (nfq *NFQueue) Valid() bool {
	return nfq.h != nil && nfq.qh != nil
}

//export go_nfq_callback
func go_nfq_callback(id uint32, hwproto uint16, hook uint8, mark *uint32,
	version, protocol, tos, ttl uint8, saddr, daddr unsafe.Pointer,
	sport, dport, checksum uint16, payloadLen uint32, payload, data unsafe.Pointer) (v uint32) {

	qidptr := (*uint16)(data)
	qid := *qidptr

	// nfq := (*NFQueue)(nfqptr)
	ipVersion := packet.IPVersion(version)
	ipsz := C.int(ipVersion.ByteSize())
	bs := C.GoBytes(payload, (C.int)(payloadLen))

	verdict := make(chan uint32, 1)
	pkt := Packet{
		QueueID:    qid,
		ID:         id,
		HWProtocol: hwproto,
		Hook:       hook,
		Mark:       *mark,
		verdict:    verdict,
		// StartedHandling: time.Now(),
	}

	// Payload
	pkt.Payload = bs

	// Info
	info := pkt.Info()
	info.Version = ipVersion
	info.InTunnel = false
	info.Protocol = packet.IPProtocol(protocol)

	// IPs
	info.Src = net.IP(C.GoBytes(saddr, ipsz))
	info.Dst = net.IP(C.GoBytes(daddr, ipsz))

	// Ports
	info.SrcPort = sport
	info.DstPort = dport

	// fmt.Printf("%s queuing packet\n", time.Now().Format("060102 15:04:05.000"))
	// BUG: "panic: send on closed channel" when shutting down
	queues[qid].Packets <- &pkt

	select {
	case v = <-pkt.verdict:
		*mark = pkt.Mark
		// *mark = 1710
	case <-time.After(queues[qid].Timeout):
		v = queues[qid].DefaultVerdict
	}

	// log.Tracef("nfqueue: took %s to handle packet", time.Now().Sub(pkt.StartedHandling).String())

	return v
}
