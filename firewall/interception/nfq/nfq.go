// +build linux

// Package nfq contains a nfqueue library experiment.
package nfq

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/safing/portbase/log"
	pmpacket "github.com/safing/portmaster/network/packet"
	"github.com/tevino/abool"
	"golang.org/x/sys/unix"

	"github.com/florianl/go-nfqueue"
)

// Queue wraps a nfqueue
type Queue struct {
	id                   uint16
	nf                   *nfqueue.Nfqueue
	packets              chan pmpacket.Packet
	cancelSocketCallback context.CancelFunc

	pendingVerdicts  uint64
	verdictCompleted chan struct{}
}

// New opens a new nfQueue.
func New(qid uint16, v6 bool) (*Queue, error) { //nolint:gocognit
	afFamily := unix.AF_INET
	if v6 {
		afFamily = unix.AF_INET6
	}
	cfg := &nfqueue.Config{
		NfQueue:      qid,
		MaxPacketLen: 1600, // mtu is normally around 1500, make sure to capture it.
		MaxQueueLen:  0xffff,
		AfFamily:     uint8(afFamily),
		Copymode:     nfqueue.NfQnlCopyPacket,
		ReadTimeout:  1000 * time.Millisecond,
		WriteTimeout: 1000 * time.Millisecond,
	}

	nf, err := nfqueue.Open(cfg)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(context.Background())
	q := &Queue{
		id:                   qid,
		nf:                   nf,
		packets:              make(chan pmpacket.Packet, 1000),
		cancelSocketCallback: cancel,
		verdictCompleted:     make(chan struct{}, 1),
	}

	fn := func(attrs nfqueue.Attribute) int {

		if attrs.PacketID == nil {
			// we need a packet id to set a verdict,
			// if we don't get an ID there's hardly anything
			// we can do.
			return 0
		}

		pkt := &packet{
			pktID:          *attrs.PacketID,
			queue:          q,
			received:       time.Now(),
			verdictSet:     make(chan struct{}),
			verdictPending: abool.New(),
		}

		if attrs.Payload != nil {
			pkt.Payload = *attrs.Payload
		}

		if err := pmpacket.Parse(pkt.Payload, pkt.Info()); err != nil {
			log.Warningf("nfqueue: failed to parse payload: %s", err)
			_ = pkt.Drop()
			return 0
		}

		select {
		case q.packets <- pkt:
			log.Tracef("nfqueue: queued packet %s (%s -> %s) after %s", pkt.ID(), pkt.Info().Src, pkt.Info().Dst, time.Since(pkt.received))
		case <-ctx.Done():
			return 0
		case <-time.After(time.Second):
			log.Warningf("nfqueue: failed to queue packet (%s since it was handed over by the kernel)", time.Since(pkt.received))
		}

		go func() {
			select {
			case <-pkt.verdictSet:

			case <-time.After(20 * time.Second):
				log.Warningf("nfqueue: no verdict set for packet %s (%s -> %s) after %s, dropping", pkt.ID(), pkt.Info().Src, pkt.Info().Dst, time.Since(pkt.received))
				if err := pkt.Drop(); err != nil {
					log.Warningf("nfqueue: failed to apply default-drop to unveridcted packet %s (%s -> %s)", pkt.ID(), pkt.Info().Src, pkt.Info().Dst)
				}
			}
		}()

		return 0 // continue calling this fn
	}

	errorFunc := func(e error) int {
		// embedded interface is required to work-around some
		// dep-vendoring weirdness
		if opError, ok := e.(interface {
			Timeout() bool
			Temporary() bool
		}); ok {
			if opError.Timeout() || opError.Temporary() {
				c := atomic.LoadUint64(&q.pendingVerdicts)
				if c > 0 {
					log.Tracef("nfqueue: waiting for %d pending verdicts", c)

					for atomic.LoadUint64(&q.pendingVerdicts) > 0 { // must NOT use c here
						<-q.verdictCompleted
					}
				}

				return 0
			}
		}
		log.Errorf("nfqueue: encountered error while receiving packets: %s\n", e.Error())

		return 1
	}

	if err := q.nf.RegisterWithErrorFunc(ctx, fn, errorFunc); err != nil {
		defer q.nf.Close()
		return nil, err
	}

	return q, nil
}

// Destroy destroys the queue. Any error encountered is logged.
func (q *Queue) Destroy() {
	q.cancelSocketCallback()

	if err := q.nf.Close(); err != nil {
		log.Errorf("nfqueue: failed to close queue %d: %s", q.id, err)
	}
}

// PacketChannel returns the packet channel.
func (q *Queue) PacketChannel() <-chan pmpacket.Packet {
	return q.packets
}
