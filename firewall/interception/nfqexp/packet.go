package nfqexp

import (
	"errors"
	"time"

	"github.com/florianl/go-nfqueue"
	"github.com/mdlayher/netlink"
	"github.com/safing/portbase/log"
	pmpacket "github.com/safing/portmaster/network/packet"
)

// Firewalling marks used by the Portmaster.
// See TODO on packet.mark() on their relevance
// and a possibility to remove most IPtables rules.
const (
	MarkAccept       = 1700
	MarkBlock        = 1701
	MarkDrop         = 1702
	MarkAcceptAlways = 1710
	MarkBlockAlways  = 1711
	MarkDropAlways   = 1712
	MarkRerouteNS    = 1799
	MarkRerouteSPN   = 1717
)

func markToString(mark int) string {
	switch mark {
	case MarkAccept:
		return "Accept"
	case MarkBlock:
		return "Block"
	case MarkDrop:
		return "Drop"
	case MarkAcceptAlways:
		return "AcceptAlways"
	case MarkBlockAlways:
		return "BlockAlways"
	case MarkDropAlways:
		return "DropAlways"
	case MarkRerouteNS:
		return "RerouteNS"
	case MarkRerouteSPN:
		return "RerouteSPN"
	}
	return "unknown"
}

// packet implements the packet.Packet interface.
type packet struct {
	pmpacket.Base
	ID         uint32
	received   time.Time
	queue      *Queue
	verdictSet chan struct{}
}

// TODO(ppacher): revisit the following behavior:
// 		The legacy implementation of nfqueue (and the interception) module
// 		always accept a packet but may mark it so that a subsequent rule in
// 		the C17 chain drops, rejects or modifies it.
//
//		For drop/return we could use the actual nfQueue verdicts Drop and Stop.
//		Re-routing to local NS or SPN can be done by modifying the packet here
//		and using SetVerdictModPacket and reject can be implemented using a simple
// 		raw-socket.
//
func (pkt *packet) mark(mark int) (err error) {
	defer func() {
		if x := recover(); x != nil {
			err = errors.New("verdict set")
		}
	}()
	for {
		if err := pkt.queue.nf.SetVerdictWithMark(pkt.ID, nfqueue.NfAccept, mark); err != nil {
			log.Warningf("nfqexp: failed to set verdict %s for %d (%s -> %s): %s", markToString(mark), pkt.ID, pkt.Info().Src, pkt.Info().Dst, err)
			if opErr, ok := err.(*netlink.OpError); ok {
				if opErr.Timeout() || opErr.Temporary() {
					continue
				}
			}

			return err
		}
		break
	}
	log.Tracef("nfqexp: marking packet %d (%s -> %s) on queue %d with %s after %s", pkt.ID, pkt.Info().Src, pkt.Info().Dst, pkt.queue.id, markToString(mark), time.Since(pkt.received))
	close(pkt.verdictSet)
	return nil
}

func (pkt *packet) Accept() error {
	return pkt.mark(MarkAccept)
}

func (pkt *packet) Block() error {
	return pkt.mark(MarkBlock)
}

func (pkt *packet) Drop() error {
	return pkt.mark(MarkDrop)
}

func (pkt *packet) PermanentAccept() error {
	return pkt.mark(MarkAcceptAlways)
}

func (pkt *packet) PermanentBlock() error {
	return pkt.mark(MarkBlockAlways)
}

func (pkt *packet) PermanentDrop() error {
	return pkt.mark(MarkDropAlways)
}

func (pkt *packet) RerouteToNameserver() error {
	return pkt.mark(MarkRerouteNS)
}

func (pkt *packet) RerouteToTunnel() error {
	return pkt.mark(MarkRerouteSPN)
}
