package captain

import (
	"time"

	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/spn/conf"
	"github.com/safing/portmaster/spn/docks"
	"github.com/safing/portmaster/spn/hub"
	"github.com/safing/portmaster/spn/terminal"
	"github.com/safing/structures/container"
	"github.com/safing/structures/varint"
)

// GossipOpType is the type ID of the gossip operation.
const GossipOpType string = "gossip"

// GossipMsgType is the gossip message type.
type GossipMsgType uint8

// Gossip Message Types.
const (
	GossipHubAnnouncementMsg GossipMsgType = 1
	GossipHubStatusMsg       GossipMsgType = 2
)

func (msgType GossipMsgType) String() string {
	switch msgType {
	case GossipHubAnnouncementMsg:
		return "hub announcement"
	case GossipHubStatusMsg:
		return "hub status"
	default:
		return "unknown gossip msg"
	}
}

// GossipOp is used to gossip Hub messages.
type GossipOp struct {
	terminal.OperationBase

	craneID string
}

// Type returns the type ID.
func (op *GossipOp) Type() string {
	return GossipOpType
}

func init() {
	terminal.RegisterOpType(terminal.OperationFactory{
		Type:     GossipOpType,
		Requires: terminal.IsCraneController,
		Start:    runGossipOp,
	})
}

// NewGossipOp start a new gossip operation.
func NewGossipOp(controller *docks.CraneControllerTerminal) (*GossipOp, *terminal.Error) {
	// Create and init.
	op := &GossipOp{
		craneID: controller.Crane.ID,
	}
	err := controller.StartOperation(op, nil, 1*time.Minute)
	if err != nil {
		return nil, err
	}
	op.InitOperationBase(controller, op.ID())

	// Register and return.
	registerGossipOp(controller.Crane.ID, op)
	return op, nil
}

func runGossipOp(t terminal.Terminal, opID uint32, data *container.Container) (terminal.Operation, *terminal.Error) {
	// Check if we are run by a controller.
	controller, ok := t.(*docks.CraneControllerTerminal)
	if !ok {
		return nil, terminal.ErrIncorrectUsage.With("gossip op may only be started by a crane controller terminal, but was started by %T", t)
	}

	// Create, init, register and return.
	op := &GossipOp{
		craneID: controller.Crane.ID,
	}
	op.InitOperationBase(t, opID)
	registerGossipOp(controller.Crane.ID, op)
	return op, nil
}

func (op *GossipOp) sendMsg(msgType GossipMsgType, data []byte) {
	// Create message.
	msg := op.NewEmptyMsg()
	msg.Data = container.New(
		varint.Pack8(uint8(msgType)),
		data,
	)
	msg.Unit.MakeHighPriority()

	// Send.
	err := op.Send(msg, 1*time.Second)
	if err != nil {
		log.Debugf("spn/captain: failed to forward %s via %s: %s", msgType, op.craneID, err)
	}
}

// Deliver delivers a message to the operation.
func (op *GossipOp) Deliver(msg *terminal.Msg) *terminal.Error {
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
		log.Warningf("spn/captain: received unknown gossip message type from %s: %d", op.craneID, gossipMsgType)
		return nil
	}

	// Import and verify.
	h, forward, tErr := docks.ImportAndVerifyHubInfo(module.mgr.Ctx(), "", announcementData, statusData, conf.MainMapName, conf.MainMapScope)
	if tErr != nil {
		if tErr.Is(hub.ErrOldData) {
			log.Debugf("spn/captain: ignoring old %s from %s", gossipMsgType, op.craneID)
		} else {
			log.Warningf("spn/captain: failed to import %s from %s: %s", gossipMsgType, op.craneID, tErr)
		}
	} else if forward {
		// Only log if we received something to save/forward.
		log.Infof("spn/captain: received %s for %s", gossipMsgType, h)
	}

	// Relay data.
	if forward {
		gossipRelayMsg(op.craneID, gossipMsgType, data)
	}
	return nil
}

// HandleStop gives the operation the ability to cleanly shut down.
// The returned error is the error to send to the other side.
// Should never be called directly. Call Stop() instead.
func (op *GossipOp) HandleStop(err *terminal.Error) (errorToSend *terminal.Error) {
	deleteGossipOp(op.craneID)
	return err
}
