package captain

import (
	"context"
	"strings"
	"time"

	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/service/mgr"
	"github.com/safing/portmaster/spn/conf"
	"github.com/safing/portmaster/spn/docks"
	"github.com/safing/portmaster/spn/hub"
	"github.com/safing/portmaster/spn/terminal"
	"github.com/safing/structures/container"
	"github.com/safing/structures/varint"
)

// GossipQueryOpType is the type ID of the gossip query operation.
const GossipQueryOpType string = "gossip/query"

// GossipQueryOp is used to query gossip messages.
type GossipQueryOp struct {
	terminal.OperationBase

	t         terminal.Terminal
	client    bool
	importCnt int

	ctx       context.Context
	cancelCtx context.CancelFunc
}

// Type returns the type ID.
func (op *GossipQueryOp) Type() string {
	return GossipQueryOpType
}

func init() {
	terminal.RegisterOpType(terminal.OperationFactory{
		Type:     GossipQueryOpType,
		Requires: terminal.IsCraneController,
		Start:    runGossipQueryOp,
	})
}

// NewGossipQueryOp starts a new gossip query operation.
func NewGossipQueryOp(t terminal.Terminal) (*GossipQueryOp, *terminal.Error) {
	// Create and init.
	op := &GossipQueryOp{
		t:      t,
		client: true,
	}
	op.ctx, op.cancelCtx = context.WithCancel(t.Ctx())
	err := t.StartOperation(op, nil, 1*time.Minute)
	if err != nil {
		return nil, err
	}
	return op, nil
}

func runGossipQueryOp(t terminal.Terminal, opID uint32, data *container.Container) (terminal.Operation, *terminal.Error) {
	// Create, init, register and return.
	op := &GossipQueryOp{t: t}
	op.ctx, op.cancelCtx = context.WithCancel(t.Ctx())
	op.InitOperationBase(t, opID)

	module.mgr.Go("gossip query handler", op.handler)

	return op, nil
}

func (op *GossipQueryOp) handler(_ *mgr.WorkerCtx) error {
	tErr := op.sendMsgs(hub.MsgTypeAnnouncement)
	if tErr != nil {
		op.Stop(op, tErr)
		return nil // Clean worker exit.
	}

	tErr = op.sendMsgs(hub.MsgTypeStatus)
	if tErr != nil {
		op.Stop(op, tErr)
		return nil // Clean worker exit.
	}

	op.Stop(op, nil)
	return nil // Clean worker exit.
}

func (op *GossipQueryOp) sendMsgs(msgType hub.MsgType) *terminal.Error {
	it, err := hub.QueryRawGossipMsgs(conf.MainMapName, msgType)
	if err != nil {
		return terminal.ErrInternalError.With("failed to query: %w", err)
	}
	defer it.Cancel()

iterating:
	for {
		select {
		case r := <-it.Next:
			// Check if we are done.
			if r == nil {
				return nil
			}

			// Ensure we're handling a hub msg.
			hubMsg, err := hub.EnsureHubMsg(r)
			if err != nil {
				log.Warningf("spn/captain: failed to load hub msg: %s", err)
				continue iterating
			}

			// Create gossip msg.
			var c *container.Container
			switch hubMsg.Type {
			case hub.MsgTypeAnnouncement:
				c = container.New(
					varint.Pack8(uint8(GossipHubAnnouncementMsg)),
					hubMsg.Data,
				)
			case hub.MsgTypeStatus:
				c = container.New(
					varint.Pack8(uint8(GossipHubStatusMsg)),
					hubMsg.Data,
				)
			default:
				log.Warningf("spn/captain: unknown hub msg for gossip query at %q: %s", hubMsg.Key(), hubMsg.Type)
			}

			// Send msg.
			if c != nil {
				msg := op.NewEmptyMsg()
				msg.Unit.MakeHighPriority()
				msg.Data = c
				tErr := op.Send(msg, 1*time.Second)
				if tErr != nil {
					return tErr.Wrap("failed to send msg")
				}
			}

		case <-op.ctx.Done():
			return terminal.ErrStopping
		}
	}
}

// Deliver delivers the message to the operation.
func (op *GossipQueryOp) Deliver(msg *terminal.Msg) *terminal.Error {
	defer msg.Finish()

	gossipMsgTypeN, err := msg.Data.GetNextN8()
	if err != nil {
		return terminal.ErrMalformedData.With("failed to parse gossip message type")
	}
	gossipMsgType := GossipMsgType(gossipMsgTypeN)

	// Prepare data.
	data := msg.Data.CompileData()
	var announcementData, statusData []byte
	switch gossipMsgType {
	case GossipHubAnnouncementMsg:
		announcementData = data
	case GossipHubStatusMsg:
		statusData = data
	default:
		log.Warningf("spn/captain: received unknown gossip message type from gossip query: %d", gossipMsgType)
		return nil
	}

	// Import and verify.
	h, forward, tErr := docks.ImportAndVerifyHubInfo(module.mgr.Ctx(), "", announcementData, statusData, conf.MainMapName, conf.MainMapScope)
	if tErr != nil {
		log.Warningf("spn/captain: failed to import %s from gossip query: %s", gossipMsgType, tErr)
	} else {
		log.Infof("spn/captain: received %s for %s from gossip query", gossipMsgType, h)
		op.importCnt++
	}

	// Relay data.
	if forward {
		// TODO: Find better way to get craneID.
		craneID := strings.SplitN(op.t.FmtID(), "#", 2)[0]
		gossipRelayMsg(craneID, gossipMsgType, data)
	}
	return nil
}

// HandleStop gives the operation the ability to cleanly shut down.
// The returned error is the error to send to the other side.
// Should never be called directly. Call Stop() instead.
func (op *GossipQueryOp) HandleStop(err *terminal.Error) (errorToSend *terminal.Error) {
	if op.client {
		log.Infof("spn/captain: gossip query imported %d entries", op.importCnt)
	}
	op.cancelCtx()
	return err
}
