// +build linux

// Package nfqexp contains a nfqueue library experiment.
package nfqexp

import (
	"context"
	"time"

	"github.com/safing/portbase/log"
	pmpacket "github.com/safing/portmaster/network/packet"
	"golang.org/x/sys/unix"

	"github.com/florianl/go-nfqueue"
)

// Queue wraps a nfqueue
type Queue struct {
	id                   uint16
	nf                   *nfqueue.Nfqueue
	packets              chan pmpacket.Packet
	cancelSocketCallback context.CancelFunc
}

// New opens a new nfQueue.
func New(qid uint16, v6 bool) (*Queue, error) {
	afFamily := unix.AF_INET
	if v6 {
		afFamily = unix.AF_INET6
	}
	cfg := &nfqueue.Config{
		NfQueue:      qid,
		MaxPacketLen: 0xffff,
		MaxQueueLen:  0xff,
		AfFamily:     uint8(afFamily),
		Copymode:     nfqueue.NfQnlCopyPacket,
		ReadTimeout:  50 * time.Millisecond,
		WriteTimeout: 50 * time.Millisecond,
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
	}

	fn := func(attrs nfqueue.Attribute) int {

		if attrs.PacketID == nil {
			// we need a packet id to set a verdict,
			// if we don't get an ID there's hardly anything
			// we can do.
			return 0
		}

		pkt := &packet{
			ID:         *attrs.PacketID,
			queue:      q,
			received:   time.Now(),
			verdictSet: make(chan struct{}),
		}

		if attrs.Payload != nil {
			pkt.Payload = *attrs.Payload
		}

		if err := pmpacket.Parse(pkt.Payload, pkt.Info()); err != nil {
			log.Warningf("nfqexp: failed to parse payload: %s", err)
			_ = pkt.Drop()
			return 0
		}

		select {
		case q.packets <- pkt:
			log.Tracef("nfqexp: queued packet %d (%s -> %s) after %s", pkt.ID, pkt.Info().Src, pkt.Info().Dst, time.Since(pkt.received))
		case <-ctx.Done():
			return 0
		case <-time.After(time.Second):
			log.Warningf("nfqexp: failed to queue packet (%s since it was handed over by the kernel)", time.Since(pkt.received))
		}

		go func() {
			select {
			case <-pkt.verdictSet:

			case <-time.After(5 * time.Second):
				log.Warningf("nfqexp: no verdict set for packet %d (%s -> %s) after %s, dropping", pkt.ID, pkt.Info().Src, pkt.Info().Dst, time.Since(pkt.received))
				if err := pkt.Drop(); err != nil {
					log.Warningf("nfqexp: failed to apply default-drop to unveridcted packet %d (%s -> %s)", pkt.ID, pkt.Info().Src, pkt.Info().Dst)
				}
			}
		}()

		return 0 // continue calling this fn
	}

	if err := q.nf.Register(ctx, fn); err != nil {
		defer q.nf.Close()
		return nil, err
	}

	return q, nil
}

// Destroy destroys the queue. Any error encountered is logged.
func (q *Queue) Destroy() {
	q.cancelSocketCallback()

	if err := q.nf.Close(); err != nil {
		log.Errorf("nfqexp: failed to close queue %d: %s", q.id, err)
	}
}

// PacketChannel returns the packet channel.
func (q *Queue) PacketChannel() <-chan pmpacket.Packet {
	return q.packets
}
