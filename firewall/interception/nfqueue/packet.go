// Copyright Safing ICS Technologies GmbH. Use of this source code is governed by the AGPL license that can be found in the LICENSE file.

package nfqueue

import (
	"fmt"

	"github.com/Safing/portmaster/network/packet"
)

var (
	ErrVerdictSentOrTimedOut error = fmt.Errorf("The verdict was already sent or timed out.")
)

const (
	NFQ_DROP   uint32 = 0 // discarded the packet
	NFQ_ACCEPT uint32 = 1 // the packet passes, continue iterations
	NFQ_STOLEN uint32 = 2 // gone away
	NFQ_QUEUE  uint32 = 3 // inject the packet into a different queue (the target queue number is in the high 16 bits of the verdict)
	NFQ_REPEAT uint32 = 4 // iterate the same cycle once more
	NFQ_STOP   uint32 = 5 // accept, but don't continue iterations
)

type Packet struct {
	packet.Base

	QueueId    uint16
	Id         uint32
	HWProtocol uint16
	Hook       uint8
	Mark       uint32

	// StartedHandling time.Time

	verdict chan uint32
}

// func (pkt *Packet) String() string {
//   return fmt.Sprintf("<Packet QId: %d, Id: %d, Type: %s, Src: %s:%d, Dst: %s:%d, Mark: 0x%X, Checksum: 0x%X, TOS: 0x%X, TTL: %d>",
//     pkt.QueueId, pkt.Id, pkt.Protocol, pkt.Src, pkt.SrcPort, pkt.Dst, pkt.DstPort, pkt.Mark, pkt.Checksum, pkt.Tos, pkt.TTL)
// }

func (pkt *Packet) setVerdict(v uint32) (err error) {
	defer func() {
		if x := recover(); x != nil {
			err = ErrVerdictSentOrTimedOut
		}
	}()
	pkt.verdict <- v
	close(pkt.verdict)
	// log.Tracef("firewall: packet %s verdict %d", pkt, v)
	return err
}

// Marks:
// 17: Identifier
// 0/1: Just this packet/this Link
// 0/1/2: Accept, Block, Drop

// func (pkt *Packet) Accept() error {
// 	return pkt.setVerdict(NFQ_STOP)
// }
//
// func (pkt *Packet) Block() error {
// 	pkt.Mark = 1701
// 	return pkt.setVerdict(NFQ_ACCEPT)
// }
//
// func (pkt *Packet) Drop() error {
// 	return pkt.setVerdict(NFQ_DROP)
// }

func (pkt *Packet) Accept() error {
	pkt.Mark = 1700
	return pkt.setVerdict(NFQ_ACCEPT)
}

func (pkt *Packet) Block() error {
	pkt.Mark = 1701
	return pkt.setVerdict(NFQ_ACCEPT)
}

func (pkt *Packet) Drop() error {
	pkt.Mark = 1702
	return pkt.setVerdict(NFQ_ACCEPT)
}

func (pkt *Packet) PermanentAccept() error {
	pkt.Mark = 1710
	return pkt.setVerdict(NFQ_ACCEPT)
}

func (pkt *Packet) PermanentBlock() error {
	pkt.Mark = 1711
	return pkt.setVerdict(NFQ_ACCEPT)
}

func (pkt *Packet) PermanentDrop() error {
	pkt.Mark = 1712
	return pkt.setVerdict(NFQ_ACCEPT)
}

func (pkt *Packet) RerouteToNameserver() error {
	pkt.Mark = 1799
	return pkt.setVerdict(NFQ_ACCEPT)
}

func (pkt *Packet) RerouteToTunnel() error {
	pkt.Mark = 1717
	return pkt.setVerdict(NFQ_ACCEPT)
}

//HUGE warning, if the iptables rules aren't set correctly this can cause some problems.
// func (pkt *Packet) Repeat() error {
// 	return this.SetVerdict(REPEAT)
// }
