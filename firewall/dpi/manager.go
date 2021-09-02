package dpi

import (
	"fmt"

	"github.com/google/gopacket"
	"github.com/google/gopacket/ip4defrag"
	"github.com/google/gopacket/layers"
	"github.com/google/gopacket/reassembly"
	"github.com/safing/portbase/log"
	"github.com/safing/portmaster/network"
	"github.com/safing/portmaster/network/packet"
)

type Manager struct {
	defragv4  *ip4defrag.IPv4Defragmenter
	pool      *reassembly.StreamPool
	assembler *reassembly.Assembler
}

func NewManager() *Manager {
	streamFactory := new(tcpStreamFactory)
	streamPool := reassembly.NewStreamPool(streamFactory)
	assembler := reassembly.NewAssembler(streamPool)

	mng := &Manager{
		defragv4:  ip4defrag.NewIPv4Defragmenter(),
		pool:      streamPool,
		assembler: assembler,
	}

	// make sure the streamFactory has a reference to the
	// manager for dispatching reassembled streams.
	streamFactory.manager = mng

	return mng
}

type named interface{ Name() string }

func (mng *Manager) HandlePacket(conn *network.Connection, p packet.Packet) (network.Verdict, network.VerdictReason, error) {
	trace := log.Tracer(p.Ctx())

	gp := p.Layers()

	// if this is a IPv4 packet make sure we defrag it.
	ipv4Layer := gp.Layer(layers.LayerTypeIPv4)
	if ipv4Layer != nil {
		ipv4 := ipv4Layer.(*layers.IPv4)
		l := ipv4.Length

		newip4, err := mng.defragv4.DefragIPv4(ipv4)
		if err != nil {
			return 0, nil, fmt.Errorf("failed to de-fragment: %w", err)
		}
		if newip4 == nil {
			// this is a fragmented packet
			// wait for the next one
			trace.Debugf("tcp-stream-manager: fragmented IPv4 packet ...")
			return 0, nil, nil
		}
		if newip4.Length != l {
			pb, ok := gp.(gopacket.PacketBuilder)
			if !ok {
				return 0, nil, fmt.Errorf("expected a PacketBuilder, got %T", p)
			}
			trace.Debugf("decoding re-assembled packet ...")
			nextDecoder := newip4.NextLayerType()
			nextDecoder.Decode(newip4.Payload, pb)
		}
	}

	var (
		verdict          network.Verdict
		reason           network.VerdictReason
		hasActiveHandler bool
	)
	for idx, pk := range conn.PacketHandlers() {
		if pk == nil {
			continue
		}
		name := fmt.Sprintf("%T", pk)
		if n, ok := pk.(named); ok {
			name = n.Name()
		}

		hasActiveHandler = true

		trace.Infof("%s: running packet inspector", name)
		v, r, err := pk.HandlePacket(conn, gp)
		if err != nil {
			trace.Errorf("inspector(%s): failed to call packet handler: %s", name, err)
		}
		if v == network.VerdictUndeterminable {
			// this handler is not applicable for conn anymore
			trace.Debugf("inspector(%s): packet inspector is not applicable for this connection anymore ...", name)
			conn.RemoveHandler(idx, pk)
			continue
		}
		if err != nil {
			continue
		}

		if v > network.VerdictUndecided {
			trace.Infof("inspector(%s): packet inspector found a conclusion: %s", name, v.String())
		}
		if v > verdict {
			verdict = v
			reason = r
		}
	}

	// handle TCP stream reassembling
	tcp := gp.Layer(layers.LayerTypeTCP)
	if tcp != nil {
		tcp := tcp.(*layers.TCP)
		c := &Context{
			CaptureInfo: gp.Metadata().CaptureInfo,
			Connection:  conn,
			Tracer:      trace,
		}

		// reassemble the stream and call any stream handlers of the connection
		mng.assembler.AssembleWithContext(gp.NetworkLayer().NetworkFlow(), tcp, c)
		if !c.HandlerExecuted {
			// if we did not even try to execute the handler
			// we need to assume there are still active ones.
			// This may happen we we are still waiting for
			// an IP frame ...
			hasActiveHandler = true
		} else {
			if c.Verdict != nil {
				hasActiveHandler = true
				if *c.Verdict > verdict {
					verdict = *c.Verdict
				}
			}
		}
	}

	udp := gp.Layer(layers.LayerTypeUDP)
	if udp != nil {
		payload := gp.ApplicationLayer().Payload()
		for idx, uh := range conn.DgramHandlers() {
			if uh == nil {
				continue
			}
			name := fmt.Sprintf("%T", uh)
			if n, ok := uh.(named); ok {
				name = n.Name()
			}

			hasActiveHandler = true
			trace.Infof("inspector(%s): running dgram inspector", name)
			v, r, err := uh.HandleDGRAM(conn, network.FlowDirection(!conn.Inbound), payload)
			if err != nil {
				trace.Errorf("inspector(%s): failed to run dgram handler: %s", name, err)
			}
			if v == network.VerdictUndeterminable {
				trace.Debugf("inspector(%s): dgram inspector is not applicable for this connection anymore ...", name)
				conn.RemoveHandler(idx, uh)
				continue
			}
			if err != nil {
				continue
			}

			if v > network.VerdictUndecided {
				trace.Infof("inspector(%s): dgram inspector found a conclusion: %s", name, v.String())
			}
			if v > verdict {
				verdict = v
				reason = r
			}
		}
	}

	if !hasActiveHandler || verdict > network.VerdictUndeterminable {
		trace.Infof("stopping inspection %s: hasActiveHandler=%v verdict=%s", conn.ID, hasActiveHandler, verdict.String())
		// we don't have any active handlers anymore so
		// there's no need to continue inspection
		conn.Inspecting = false
	}

	return verdict, reason, nil
}
