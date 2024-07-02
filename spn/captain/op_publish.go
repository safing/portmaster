package captain

import (
	"time"

	"github.com/safing/portmaster/spn/cabin"
	"github.com/safing/portmaster/spn/conf"
	"github.com/safing/portmaster/spn/docks"
	"github.com/safing/portmaster/spn/hub"
	"github.com/safing/portmaster/spn/terminal"
	"github.com/safing/structures/container"
)

// PublishOpType is the type ID of the publish operation.
const PublishOpType string = "publish"

// PublishOp is used to publish a connection.
type PublishOp struct {
	terminal.OperationBase
	controller *docks.CraneControllerTerminal

	identity      *cabin.Identity
	requestingHub *hub.Hub
	verification  *cabin.Verification
	result        chan *terminal.Error
}

// Type returns the type ID.
func (op *PublishOp) Type() string {
	return PublishOpType
}

func init() {
	terminal.RegisterOpType(terminal.OperationFactory{
		Type:     PublishOpType,
		Requires: terminal.IsCraneController,
		Start:    runPublishOp,
	})
}

// NewPublishOp start a new publish operation.
func NewPublishOp(controller *docks.CraneControllerTerminal, identity *cabin.Identity) (*PublishOp, *terminal.Error) {
	// Create and init.
	op := &PublishOp{
		controller: controller,
		identity:   identity,
		result:     make(chan *terminal.Error, 1),
	}
	msg := container.New()

	// Add Hub Announcement.
	announcementData, err := identity.ExportAnnouncement()
	if err != nil {
		return nil, terminal.ErrInternalError.With("failed to export announcement: %w", err)
	}
	msg.AppendAsBlock(announcementData)

	// Add Hub Status.
	statusData, err := identity.ExportStatus()
	if err != nil {
		return nil, terminal.ErrInternalError.With("failed to export status: %w", err)
	}
	msg.AppendAsBlock(statusData)

	tErr := controller.StartOperation(op, msg, 10*time.Second)
	if tErr != nil {
		return nil, tErr
	}
	return op, nil
}

func runPublishOp(t terminal.Terminal, opID uint32, data *container.Container) (terminal.Operation, *terminal.Error) {
	// Check if we are run by a controller.
	controller, ok := t.(*docks.CraneControllerTerminal)
	if !ok {
		return nil, terminal.ErrIncorrectUsage.With("publish op may only be started by a crane controller terminal, but was started by %T", t)
	}

	// Parse and import Announcement and Status.
	announcementData, err := data.GetNextBlock()
	if err != nil {
		return nil, terminal.ErrMalformedData.With("failed to get announcement: %w", err)
	}
	statusData, err := data.GetNextBlock()
	if err != nil {
		return nil, terminal.ErrMalformedData.With("failed to get status: %w", err)
	}
	h, forward, tErr := docks.ImportAndVerifyHubInfo(module.mgr.Ctx(), "", announcementData, statusData, conf.MainMapName, conf.MainMapScope)
	if tErr != nil {
		return nil, tErr.Wrap("failed to import and verify hub")
	}
	// Update reference in case it was changed by the import.
	controller.Crane.ConnectedHub = h

	// Relay data.
	if forward {
		gossipRelayMsg(controller.Crane.ID, GossipHubAnnouncementMsg, announcementData)
		gossipRelayMsg(controller.Crane.ID, GossipHubStatusMsg, statusData)
	}

	// Create verification request.
	v, request, err := cabin.CreateVerificationRequest(PublishOpType, "", "")
	if err != nil {
		return nil, terminal.ErrInternalError.With("failed to create verification request: %w", err)
	}

	// Create operation.
	op := &PublishOp{
		controller:    controller,
		requestingHub: h,
		verification:  v,
		result:        make(chan *terminal.Error, 1),
	}
	op.InitOperationBase(controller, opID)

	// Reply with verification request.
	tErr = op.Send(op.NewMsg(request), 10*time.Second)
	if tErr != nil {
		return nil, tErr.Wrap("failed to send verification request")
	}

	return op, nil
}

// Deliver delivers a message to the operation.
func (op *PublishOp) Deliver(msg *terminal.Msg) *terminal.Error {
	defer msg.Finish()

	if op.identity != nil {
		// Client

		// Sign the received verification request.
		response, err := op.identity.SignVerificationRequest(msg.Data.CompileData(), PublishOpType, "", "")
		if err != nil {
			return terminal.ErrPermissionDenied.With("signing verification request failed: %w", err)
		}

		return op.Send(op.NewMsg(response), 10*time.Second)
	} else if op.requestingHub != nil {
		// Server

		// Verify the signed request.
		err := op.verification.Verify(msg.Data.CompileData(), op.requestingHub)
		if err != nil {
			return terminal.ErrPermissionDenied.With("checking verification request failed: %w", err)
		}
		return terminal.ErrExplicitAck
	}

	return terminal.ErrInternalError.With("invalid operation state")
}

// Result returns the result (end error) of the operation.
func (op *PublishOp) Result() <-chan *terminal.Error {
	return op.result
}

// HandleStop gives the operation the ability to cleanly shut down.
// The returned error is the error to send to the other side.
// Should never be called directly. Call Stop() instead.
func (op *PublishOp) HandleStop(tErr *terminal.Error) (errorToSend *terminal.Error) {
	if tErr.Is(terminal.ErrExplicitAck) {
		// TODO: Check for concurrenct access.
		if op.controller.Crane.ConnectedHub == nil {
			op.controller.Crane.ConnectedHub = op.requestingHub
		}

		// Publish crane, abort if it fails.
		err := op.controller.Crane.Publish()
		if err != nil {
			tErr = terminal.ErrInternalError.With("failed to publish crane: %w", err)
			op.controller.Crane.Stop(tErr)
		} else {
			op.controller.Crane.NotifyUpdate()
		}
	}

	select {
	case op.result <- tErr:
	default:
	}
	return tErr
}
