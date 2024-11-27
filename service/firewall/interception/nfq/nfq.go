//go:build linux

// Package nfq contains a nfqueue library experiment.
package nfq

import (
	"context"
	"runtime"
	"strings"
	"sync/atomic"
	"time"

	"github.com/florianl/go-nfqueue"
	"github.com/tevino/abool"
	"golang.org/x/sys/unix"

	"github.com/safing/portmaster/base/log"
	pmpacket "github.com/safing/portmaster/service/network/packet"
	"github.com/safing/portmaster/service/process"
)

// Queue wraps a nfqueue.
type Queue struct {
	id                   uint16
	afFamily             uint8
	nf                   atomic.Value
	packets              chan pmpacket.Packet
	cancelSocketCallback context.CancelFunc
	restart              chan struct{}

	pendingVerdicts  uint64
	verdictCompleted chan struct{}
}

func (q *Queue) getNfq() *nfqueue.Nfqueue {
	return q.nf.Load().(*nfqueue.Nfqueue) //nolint:forcetypeassert // TODO: Check.
}

// New opens a new nfQueue.
func New(qid uint16, v6 bool) (*Queue, error) { //nolint:gocognit
	afFamily := unix.AF_INET
	if v6 {
		afFamily = unix.AF_INET6
	}

	ctx, cancel := context.WithCancel(context.Background())
	q := &Queue{
		id:                   qid,
		afFamily:             uint8(afFamily),
		nf:                   atomic.Value{},
		restart:              make(chan struct{}, 1),
		packets:              make(chan pmpacket.Packet, 1000),
		cancelSocketCallback: cancel,
		verdictCompleted:     make(chan struct{}, 1),
	}

	// Do not retry if the first one fails immediately as it
	// might point to a deeper integration error that's not fixable
	// with retrying ...
	if err := q.open(ctx); err != nil {
		return nil, err
	}

	go func() {
	Wait:
		for {
			select {
			case <-ctx.Done():
				return
			case <-q.restart:
				runtime.Gosched()
			}

			for {
				err := q.open(ctx)
				if err == nil {
					continue Wait
				}

				// Wait 100 ms and then try again ...
				log.Errorf("Failed to open nfqueue: %s", err)
				select {
				case <-ctx.Done():
					return
				case <-time.After(100 * time.Millisecond):
				}
			}
		}
	}()

	return q, nil
}

// open opens a new netlink socket and creates a new nfqueue.
// Upon success, the new nfqueue is atomically stored in Queue.nf.
// Users must use Queue.getNfq to access it. open does not care about
// any other value or queue that might be stored in Queue.nf at
// the time open is called.
func (q *Queue) open(ctx context.Context) error {
	cfg := &nfqueue.Config{
		NfQueue:      q.id,
		MaxPacketLen: 1600, // mtu is normally around 1500, make sure to capture it.
		MaxQueueLen:  0xffff,
		AfFamily:     q.afFamily,
		Copymode:     nfqueue.NfQnlCopyPacket,
		ReadTimeout:  1000 * time.Millisecond,
		WriteTimeout: 1000 * time.Millisecond,
	}

	nf, err := nfqueue.Open(cfg)
	if err != nil {
		return err
	}

	if err := nf.RegisterWithErrorFunc(ctx, q.packetHandler(ctx), q.handleError); err != nil {
		_ = nf.Close()
		return err
	}

	q.nf.Store(nf)

	return nil
}

func (q *Queue) handleError(e error) int {
	// embedded interface is required to work-around some
	// dep-vendoring weirdness
	if opError, ok := e.(interface { //nolint:errorlint // TODO: Check if we can remove workaround.
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

	// Check if the queue was already closed. Unfortunately, the exposed error
	// variable is in an internal stdlib package. Therefore, check for the error
	// string instead. :(
	// Official error variable is defined here:
	// https://github.com/golang/go/blob/0e85fd7561de869add933801c531bf25dee9561c/src/internal/poll/fd.go#L24
	if !strings.HasSuffix(e.Error(), "use of closed file") {
		log.Errorf("nfqueue: encountered error while receiving packets: %s\n", e.Error())
	}

	// Close the existing socket
	if nf := q.getNfq(); nf != nil {
		// Call Close() on the Con directly, as nf.Close() calls waitgroup.Wait(), which then may deadlock.
		_ = nf.Con.Close()
	}

	// Trigger a restart of the queue
	q.restart <- struct{}{}

	return 1
}

func (q *Queue) packetHandler(ctx context.Context) func(nfqueue.Attribute) int {
	return func(attrs nfqueue.Attribute) int {
		if attrs.PacketID == nil {
			// we need a packet id to set a verdict,
			// if we don't get an ID there's hardly anything
			// we can do.
			return 0
		}

		pkt := &packet{
			pktID:          *attrs.PacketID,
			queue:          q,
			verdictSet:     make(chan struct{}),
			verdictPending: abool.New(),
		}
		pkt.Info().PID = process.UndefinedProcessID
		pkt.Info().SeenAt = time.Now()

		if attrs.Payload == nil {
			// There is not payload.
			log.Warningf("nfqueue: packet #%d has no payload", pkt.pktID)
			return 0
		}

		if err := pmpacket.ParseLayer3(*attrs.Payload, &pkt.Base); err != nil {
			log.Warningf("nfqueue: failed to parse payload: %s", err)
			_ = pkt.Drop()
			return 0
		}

		select {
		case q.packets <- pkt:
			// DEBUG:
			// log.Tracef("nfqueue: queued packet %s (%s -> %s) after %s", pkt.ID(), pkt.Info().Src, pkt.Info().Dst, time.Since(pkt.Info().SeenAt))
		case <-ctx.Done():
			return 0
		case <-time.After(time.Second):
			log.Warningf("nfqueue: failed to queue packet (%s since it was handed over by the kernel)", time.Since(pkt.Info().SeenAt))
		}

		go func() {
			select {
			case <-pkt.verdictSet:

			case <-time.After(20 * time.Second):
				log.Warningf("nfqueue: no verdict set for packet %s (%s -> %s) after %s, dropping", pkt.ID(), pkt.Info().Src, pkt.Info().Dst, time.Since(pkt.Info().SeenAt))
				if err := pkt.Drop(); err != nil {
					log.Warningf("nfqueue: failed to apply default-drop to unveridcted packet %s (%s -> %s)", pkt.ID(), pkt.Info().Src, pkt.Info().Dst)
				}
			}
		}()

		return 0 // continue calling this fn
	}
}

// Destroy destroys the queue. Any error encountered is logged.
func (q *Queue) Destroy() {
	if q == nil {
		return
	}

	q.cancelSocketCallback()

	if nf := q.getNfq(); nf != nil {
		if err := nf.Close(); err != nil {
			log.Errorf("nfqueue: failed to close queue %d: %s", q.id, err)
		}
	}
}

// PacketChannel returns the packet channel.
func (q *Queue) PacketChannel() <-chan pmpacket.Packet {
	return q.packets
}
