package dpi

import (
	"fmt"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"github.com/google/gopacket/reassembly"
	"github.com/safing/portbase/log"
	"github.com/safing/portmaster/network"
)

// Context implements reassembly.AssemblerContext.
type Context struct {
	CaptureInfo     gopacket.CaptureInfo
	Connection      *network.Connection
	Verdict         *network.Verdict
	Reason          network.VerdictReason
	Tracer          *log.ContextTracer
	HandlerExecuted bool
}

func (c *Context) GetCaptureInfo() gopacket.CaptureInfo {
	return c.CaptureInfo
}

type tcpStreamFactory struct {
	manager *Manager
}

func (factory *tcpStreamFactory) New(net, transport gopacket.Flow, tcp *layers.TCP, ac reassembly.AssemblerContext) reassembly.Stream {
	log.Infof("tcp-stream-factory: new stream for %s %s", net, transport)
	fsmOptions := reassembly.TCPSimpleFSMOptions{
		SupportMissingEstablishment: true,
	}
	stream := &tcpStream{
		net:        net,
		transport:  transport,
		tcpstate:   reassembly.NewTCPSimpleFSM(fsmOptions),
		ident:      fmt.Sprintf("%s:%s", net, transport),
		optchecker: reassembly.NewTCPOptionCheck(),
		manager:    factory.manager,
	}
	return stream
}

type tcpStream struct {
	conn    *network.Connection
	manager *Manager

	tcpstate       *reassembly.TCPSimpleFSM
	optchecker     reassembly.TCPOptionCheck
	net, transport gopacket.Flow
	ident          string
}

func (t *tcpStream) Accept(
	tcp *layers.TCP,
	ci gopacket.CaptureInfo,
	dir reassembly.TCPFlowDirection,
	nextSeq reassembly.Sequence,
	start *bool,
	ac reassembly.AssemblerContext) bool {

	conn := ac.(*Context).Connection
	if t.conn != nil && t.conn.ID != conn.ID {
		// TODO(ppacher): for localhost to localhost connections this stream-reassembler will be called for both
		// connections because gopacket's flow IDs collide with it's reverse tuple. That's on purpose so
		// client->server and server->client packets are attributed correctly but it may cause errors if the
		// portmaster sees both, the client and server side of a connection
		if t.conn.LocalPort != conn.Entity.Port {
			panic(fmt.Sprintf("TCPStream already has a connection object assigned: %s != %s", t.conn.ID, conn.ID))
		}
	}
	t.conn = conn

	if !t.tcpstate.CheckState(tcp, dir) {
		log.Errorf("tcp-stream %s: fsm: packet rejected by FSM (state: %s)", t.ident, t.tcpstate.String())
		return false
	}

	err := t.optchecker.Accept(tcp, ci, dir, nextSeq, start)
	if err != nil {
		log.Errorf("tcp-stream %s: option-checker: packet rejected: %s", t.ident, err)
		return false
	}

	return true
}

func (t *tcpStream) ReassembledSG(sg reassembly.ScatterGather, ac reassembly.AssemblerContext) {
	c := ac.(*Context)
	conn := c.Connection

	dir, start, end, skip := sg.Info()
	length, saved := sg.Lengths()
	sgStats := sg.Stats()

	data := sg.Fetch(length)
	var ident string
	if dir == reassembly.TCPDirClientToServer {
		ident = fmt.Sprintf("%v %v(%s): ", t.net, t.transport, dir)
		conn.OutgoingStream.Append(data)
	} else {
		ident = fmt.Sprintf("%v %v(%s): ", t.net.Reverse(), t.transport.Reverse(), dir)
		conn.IncomingStream.Append(data)
	}

	c.Tracer.Debugf("tcp-stream %s: reassembled packet with %d bytes (start:%v,end:%v,skip:%d,saved:%d,nb:%d,%d,overlap:%d,%d)", ident, length, start, end, skip, saved, sgStats.Packets, sgStats.Chunks, sgStats.OverlapBytes, sgStats.OverlapPackets)

	var (
		verdict    network.Verdict
		reason     network.VerdictReason
		hasHandler bool
	)
	c.HandlerExecuted = true

	all := conn.StreamHandlers()
	for idx, sh := range all {
		if sh == nil {
			continue
		}
		name := fmt.Sprintf("%T", sh)
		if n, ok := sh.(named); ok {
			name = n.Name()
		}

		hasHandler = true
		c.Tracer.Infof("inspector(%s, %d/%d): running stream inspector", name, idx+1, len(all))
		v, r, err := sh.HandleStream(conn, network.FlowDirection(dir), data)
		if err != nil {
			c.Tracer.Errorf("inspector(%s): failed to run stream handler: %s", name, err)
		}
		if v == network.VerdictUndeterminable {
			// not applicable anymore
			c.Tracer.Debugf("inspector(%s): stream inspector is not applicable anymore", name)
			conn.RemoveHandler(idx, sh)
			continue
		}
		if err != nil {
			continue
		}

		if v > network.VerdictUndecided {
			c.Tracer.Infof("inspector(%s): stream inspector found a conclusion: %s", name, v.String())
		}
		if v > verdict {
			verdict = v
			reason = r
		}
	}
	if hasHandler {
		c.Verdict = &verdict
		c.Reason = reason
	}
}

func (t *tcpStream) ReassemblyComplete(ac reassembly.AssemblerContext) bool {
	log.Infof("tcp-stream %s: connection closed", t.ident)

	if t.conn == nil {
		return true
	}

	log.Infof("tcp-stream: connection %s sent %d bytes and received %d bytes", t.conn.OutgoingStream.Length(), t.conn.IncomingStream.Length())

	return true
}
