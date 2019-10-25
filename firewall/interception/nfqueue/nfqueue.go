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

func (this *NFQueue) init() error {
	var err error
	if this.h, err = C.nfq_open(); err != nil || this.h == nil {
		return fmt.Errorf("could not open nfqueue: %s", err)
	}

	//if this.qh, err = C.nfq_create_queue(this.h, qid, C.get_cb(), unsafe.Pointer(nfq)); err != nil || this.qh == nil {

	this.Packets = make(chan packet.Packet, 1)

	if C.nfq_unbind_pf(this.h, C.AF_INET) < 0 {
		this.Destroy()
		return errors.New("nfq_unbind_pf(AF_INET) failed, are you root?")
	}
	if C.nfq_unbind_pf(this.h, C.AF_INET6) < 0 {
		this.Destroy()
		return errors.New("nfq_unbind_pf(AF_INET6) failed")
	}

	if C.nfq_bind_pf(this.h, C.AF_INET) < 0 {
		this.Destroy()
		return errors.New("nfq_bind_pf(AF_INET) failed")
	}
	if C.nfq_bind_pf(this.h, C.AF_INET6) < 0 {
		this.Destroy()
		return errors.New("nfq_bind_pf(AF_INET6) failed")
	}

	if this.qh, err = C.create_queue(this.h, C.uint16_t(this.qid)); err != nil || this.qh == nil {
		C.nfq_close(this.h)
		return fmt.Errorf("could not create queue: %s", err)
	}

	this.fd = int(C.nfq_fd(this.h))

	if C.nfq_set_mode(this.qh, C.NFQNL_COPY_PACKET, 0xffff) < 0 {
		this.Destroy()
		return errors.New("nfq_set_mode(NFQNL_COPY_PACKET) failed")
	}
	if C.nfq_set_queue_maxlen(this.qh, 1024*8) < 0 {
		this.Destroy()
		return errors.New("nfq_set_queue_maxlen(1024 * 8) failed")
	}

	return nil
}

func (this *NFQueue) Destroy() {
	this.lk.Lock()
	defer this.lk.Unlock()

	if this.fd != 0 && this.Valid() {
		syscall.Close(this.fd)
	}
	if this.qh != nil {
		C.nfq_destroy_queue(this.qh)
		this.qh = nil
	}
	if this.h != nil {
		C.nfq_close(this.h)
		this.h = nil
	}

	// TODO: don't close, we're exiting anyway
	// if this.Packets != nil {
	// 	close(this.Packets)
	// }
}

func (this *NFQueue) Valid() bool {
	return this.h != nil && this.qh != nil
}

//export go_nfq_callback
func go_nfq_callback(id uint32, hwproto uint16, hook uint8, mark *uint32,
	version, protocol, tos, ttl uint8, saddr, daddr unsafe.Pointer,
	sport, dport, checksum uint16, payload_len uint32, payload, data unsafe.Pointer) (v uint32) {

	qidptr := (*uint16)(data)
	qid := uint16(*qidptr)

	// nfq := (*NFQueue)(nfqptr)
	ipVersion := packet.IPVersion(version)
	ipsz := C.int(ipVersion.ByteSize())
	bs := C.GoBytes(payload, (C.int)(payload_len))

	verdict := make(chan uint32, 1)
	pkt := Packet{
		QueueId:    qid,
		Id:         id,
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
