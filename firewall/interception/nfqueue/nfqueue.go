// Copyright Safing ICS Technologies GmbH. Use of this source code is governed by the AGPL license that can be found in the LICENSE file.

package nfqueue

/*
#cgo LDFLAGS: -lnetfilter_queue
#cgo CFLAGS: -Wall
#include "nfqueue.h"
*/
import "C"

import (
	"net"
	"os"
	"runtime"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/Safing/safing-core/network/packet"
)

var queues map[uint16]*nfQueue

func init() {
	queues = make(map[uint16]*nfQueue)
}

type nfQueue struct {
	DefaultVerdict uint32
	Timeout        time.Duration
	qid            uint16
	qidptr         *uint16
	h              *C.struct_nfq_handle
	//qh             *C.struct_q_handle
	qh *C.struct_nfq_q_handle
	fd int
	lk sync.Mutex

	pktch chan packet.Packet
}

func NewNFQueue(qid uint16) (nfq *nfQueue) {
	if os.Geteuid() != 0 {
		panic("Must be ran by root.")
	}
	nfq = &nfQueue{DefaultVerdict: NFQ_ACCEPT, Timeout: 100 * time.Millisecond, qid: qid, qidptr: &qid}
	queues[nfq.qid] = nfq
	return nfq
}

/*
This returns a channel that will recieve packets,
the user then must call pkt.Accept() or pkt.Drop()
*/
func (this *nfQueue) Process() <-chan packet.Packet {
	if this.h != nil {
		return this.pktch
	}
	this.init()

	go func() {
		runtime.LockOSThread()
		C.loop_for_packets(this.h)
	}()

	return this.pktch
}

func (this *nfQueue) init() {
	var err error
	if this.h, err = C.nfq_open(); err != nil || this.h == nil {
		panic(err)
	}

	//if this.qh, err = C.nfq_create_queue(this.h, qid, C.get_cb(), unsafe.Pointer(nfq)); err != nil || this.qh == nil {

	this.pktch = make(chan packet.Packet, 1)

	if C.nfq_unbind_pf(this.h, C.AF_INET) < 0 {
		this.Destroy()
		panic("nfq_unbind_pf(AF_INET) failed, are you running root?.")
	}
	if C.nfq_unbind_pf(this.h, C.AF_INET6) < 0 {
		this.Destroy()
		panic("nfq_unbind_pf(AF_INET6) failed.")
	}

	if C.nfq_bind_pf(this.h, C.AF_INET) < 0 {
		this.Destroy()
		panic("nfq_bind_pf(AF_INET) failed.")
	}

	if C.nfq_bind_pf(this.h, C.AF_INET6) < 0 {
		this.Destroy()
		panic("nfq_bind_pf(AF_INET6) failed.")
	}

	if this.qh, err = C.create_queue(this.h, C.uint16_t(this.qid)); err != nil || this.qh == nil {
		C.nfq_close(this.h)
		panic(err)
	}

	this.fd = int(C.nfq_fd(this.h))

	if C.nfq_set_mode(this.qh, C.NFQNL_COPY_PACKET, 0xffff) < 0 {
		this.Destroy()
		panic("nfq_set_mode(NFQNL_COPY_PACKET) failed.")
	}
	if C.nfq_set_queue_maxlen(this.qh, 1024*8) < 0 {
		this.Destroy()
		panic("nfq_set_queue_maxlen(1024 * 8) failed.")
	}
}

func (this *nfQueue) Destroy() {
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
	// if this.pktch != nil {
	// 	close(this.pktch)
	// }
}

func (this *nfQueue) Valid() bool {
	return this.h != nil && this.qh != nil
}

//export go_nfq_callback
func go_nfq_callback(id uint32, hwproto uint16, hook uint8, mark *uint32,
	version, protocol, tos, ttl uint8, saddr, daddr unsafe.Pointer,
	sport, dport, checksum uint16, payload_len uint32, payload, data unsafe.Pointer) (v uint32) {

	qidptr := (*uint16)(data)
	qid := uint16(*qidptr)

	// nfq := (*nfQueue)(nfqptr)
	new_version := version
	ipver := packet.IPVersion(new_version)
	ipsz := C.int(ipver.ByteSize())
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

	// IPHeader
	pkt.IPHeader = &packet.IPHeader{
		Version:  4,
		Protocol: packet.IPProtocol(protocol),
		Tos:      tos,
		TTL:      ttl,
		Src:      net.IP(C.GoBytes(saddr, ipsz)),
		Dst:      net.IP(C.GoBytes(daddr, ipsz)),
	}

	// TCPUDPHeader
	pkt.TCPUDPHeader = &packet.TCPUDPHeader{
		SrcPort:  sport,
		DstPort:  dport,
		Checksum: checksum,
	}

	// fmt.Printf("%s queuing packet\n", time.Now().Format("060102 15:04:05.000"))
	// BUG: "panic: send on closed channel" when shutting down
	queues[qid].pktch <- &pkt

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
