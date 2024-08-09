package network

import (
	"context"
	"time"

	"github.com/tevino/abool"

	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/service/mgr"
	"github.com/safing/portmaster/service/network/packet"
)

// SetFirewallHandler sets the firewall handler for this link, and starts a
// worker to handle the packets.
// The caller needs to hold a lock on the connection.
// Cannot be called with "nil" handler. Call StopFirewallHandler() instead.
func (conn *Connection) SetFirewallHandler(handler FirewallHandler) {
	if handler == nil {
		return
	}

	// Initialize packet queue, if needed.
	conn.pktQueueLock.Lock()
	defer conn.pktQueueLock.Unlock()
	if !conn.pktQueueActive {
		conn.pktQueue = make(chan packet.Packet, 100)
		conn.pktQueueActive = true
	}

	// Start packet handler worker when new handler is set.
	if conn.firewallHandler == nil {
		module.mgr.Go("packet handler", conn.packetHandlerWorker)
	}

	// Set new handler.
	conn.firewallHandler = handler
}

// UpdateFirewallHandler sets the firewall handler if it already set and the
// given handler is not nil.
// The caller needs to hold a lock on the connection.
func (conn *Connection) UpdateFirewallHandler(handler FirewallHandler) {
	if handler != nil && conn.firewallHandler != nil {
		conn.firewallHandler = handler
	}
}

// StopFirewallHandler unsets the firewall handler and stops the handler worker.
// The caller needs to hold a lock on the connection.
func (conn *Connection) StopFirewallHandler() {
	conn.pktQueueLock.Lock()
	defer conn.pktQueueLock.Unlock()

	// Unset the firewall handler to revert to the default handler.
	conn.firewallHandler = nil

	// Signal the packet handler worker that it can stop.
	if conn.pktQueueActive {
		close(conn.pktQueue)
		conn.pktQueueActive = false
	}

	// Unset the packet queue so that it can be freed.
	conn.pktQueue = nil
}

// HandlePacket queues packet of Link for handling.
func (conn *Connection) HandlePacket(pkt packet.Packet) {
	// Update last seen timestamp.
	conn.lastSeen.Store(time.Now().Unix())

	conn.pktQueueLock.Lock()
	defer conn.pktQueueLock.Unlock()

	// execute handler or verdict
	if conn.pktQueueActive {
		select {
		case conn.pktQueue <- pkt:
		default:
			log.Debugf(
				"filter: dropping packet %s, as there is no space in the connection's handling queue",
				pkt,
			)
			_ = pkt.Drop()
		}
	} else {
		// Run default handler.
		defaultFirewallHandler(conn, pkt)

		// Record metrics.
		packetHandlingHistogram.UpdateDuration(pkt.Info().SeenAt)
	}
}

var infoOnlyPacketsActive = abool.New()

// packetHandlerWorker sequentially handles queued packets.
func (conn *Connection) packetHandlerWorker(ctx *mgr.WorkerCtx) error {
	// Copy packet queue, so we can remove the reference from the connection
	// when we stop the firewall handler.
	var pktQueue chan packet.Packet
	func() {
		conn.pktQueueLock.Lock()
		defer conn.pktQueueLock.Unlock()
		pktQueue = conn.pktQueue
	}()

	// pktSeq counts the seen packets.
	var pktSeq int

	for {
		select {
		case pkt := <-pktQueue:
			if pkt == nil {
				return nil
			}
			pktSeq++

			// Attempt to optimize packet handling order by handling info-only packets first.
			switch {
			case pktSeq > 1:
				// Order correction is only for first packet.

			case pkt.InfoOnly():
				// Correct order only if first packet is not info-only.

				// We have observed a first packet that is info-only.
				// Info-only packets seem to be active and working.
				infoOnlyPacketsActive.Set()

			case pkt.ExpectInfo():
				// Packet itself tells us that we should expect an info-only packet.
				fallthrough

			case infoOnlyPacketsActive.IsSet() && pkt.IsOutbound():
				// Info-only packets are active and the packet is outbound.
				// The probability is high that we will also get an info-only packet for this connection.
				// TODO: Do not do this for forwarded packets in the future.

				// DEBUG:
				// log.Debugf("filter: waiting for info only packet in order to pull forward: %s", pkt)
				select {
				case infoPkt := <-pktQueue:
					if infoPkt != nil {
						// DEBUG:
						// log.Debugf("filter: packet #%d [pulled forward] info=%v PID=%d packet: %s", pktSeq, infoPkt.InfoOnly(), infoPkt.Info().PID, pkt)
						packetHandlerHandleConn(ctx.Ctx(), conn, infoPkt)
						pktSeq++
					}
				case <-time.After(1 * time.Millisecond):
				}
			}

			// DEBUG:
			// switch {
			// case pkt.Info().Inbound:
			// 	log.Debugf("filter: packet #%d info=%v PID=%d packet: %s", pktSeq, pkt.InfoOnly(), pkt.Info().PID, pkt)
			// case pktSeq == 1 && !pkt.InfoOnly():
			// 	log.Warningf("filter: packet #%d [should be info only!] info=%v PID=%d packet: %s", pktSeq, pkt.InfoOnly(), pkt.Info().PID, pkt)
			// case pktSeq >= 2 && pkt.InfoOnly():
			// 	log.Errorf("filter: packet #%d [should not be info only!] info=%v PID=%d packet: %s", pktSeq, pkt.InfoOnly(), pkt.Info().PID, pkt)
			// default:
			// 	log.Debugf("filter: packet #%d info=%v PID=%d packet: %s", pktSeq, pkt.InfoOnly(), pkt.Info().PID, pkt)
			// }

			packetHandlerHandleConn(ctx.Ctx(), conn, pkt)

		case <-ctx.Done():
			return nil
		}
	}
}

func packetHandlerHandleConn(ctx context.Context, conn *Connection, pkt packet.Packet) {
	conn.Lock()
	defer conn.Unlock()

	// Check if we should use the default handler.
	// The default handler is only for fully decided
	// connections and just applying the verdict.
	// There is no logging for these packets.
	if conn.firewallHandler == nil {
		// Run default handler.
		defaultFirewallHandler(conn, pkt)

		// Record metrics.
		packetHandlingHistogram.UpdateDuration(pkt.Info().SeenAt)

		return
	}

	// Create tracing context.
	// Add context tracer and set context on packet.
	traceCtx, tracer := log.AddTracer(ctx)
	if tracer != nil {
		// The trace is submitted in `network.Connection.packetHandler()`.
		tracer.Tracef("filter: handling packet: %s", pkt)
	}
	pkt.SetCtx(traceCtx)

	// Handle packet with set handler.
	conn.firewallHandler(conn, pkt)

	// Record metrics.
	packetHandlingHistogram.UpdateDuration(pkt.Info().SeenAt)

	// Log result and submit trace, when there are any changes.
	if conn.saveWhenFinished {
		switch {
		case conn.DataIsComplete():
			tracer.Infof("filter: connection %s %s: %s", conn, conn.VerdictVerb(), conn.Reason.Msg)
		case conn.Verdict != VerdictUndecided:
			tracer.Debugf("filter: connection %s fast-tracked", pkt)
		default:
			tracer.Debugf("filter: gathered data on connection %s", conn)
		}
		// Submit trace logs.
		tracer.Submit()
	}

	// Push changes, if there are any.
	if conn.saveWhenFinished {
		conn.saveWhenFinished = false
		conn.Save()
	}
}
