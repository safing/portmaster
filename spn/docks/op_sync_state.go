package docks

import (
	"context"
	"time"

	"github.com/safing/portmaster/service/mgr"
	"github.com/safing/portmaster/spn/conf"
	"github.com/safing/portmaster/spn/terminal"
	"github.com/safing/structures/container"
	"github.com/safing/structures/dsd"
)

// SyncStateOpType is the type ID of the sync state operation.
const SyncStateOpType = "sync/state"

// SyncStateOp is used to sync the crane state.
type SyncStateOp struct {
	terminal.OneOffOperationBase
}

// SyncStateMessage holds the sync data.
type SyncStateMessage struct {
	Stopping        bool
	RequestStopping bool
}

// Type returns the type ID.
func (op *SyncStateOp) Type() string {
	return SyncStateOpType
}

func init() {
	terminal.RegisterOpType(terminal.OperationFactory{
		Type:     SyncStateOpType,
		Requires: terminal.IsCraneController,
		Start:    runSyncStateOp,
	})
}

// startSyncStateOp starts a worker that runs the sync state operation.
func (crane *Crane) startSyncStateOp() {
	module.mgr.Go("sync crane state", func(wc *mgr.WorkerCtx) error {
		tErr := crane.Controller.SyncState(wc.Ctx())
		if tErr != nil {
			return tErr
		}

		return nil
	})
}

// SyncState runs a sync state operation.
func (controller *CraneControllerTerminal) SyncState(ctx context.Context) *terminal.Error {
	// Check if we are a public Hub, whether we own the crane and whether the lane is public too.
	if !conf.PublicHub() || !controller.Crane.Public() {
		return nil
	}

	// Create and init.
	op := &SyncStateOp{}
	op.Init()

	// Get optimization states.
	requestStopping := false
	func() {
		controller.Crane.NetState.lock.Lock()
		defer controller.Crane.NetState.lock.Unlock()

		requestStopping = controller.Crane.NetState.stoppingRequested
	}()

	// Create sync message.
	msg := &SyncStateMessage{
		Stopping:        controller.Crane.stopping.IsSet(),
		RequestStopping: requestStopping,
	}
	data, err := dsd.Dump(msg, dsd.CBOR)
	if err != nil {
		return terminal.ErrInternalError.With("%w", err)
	}

	// Send message.
	tErr := controller.StartOperation(op, container.New(data), 30*time.Second)
	if tErr != nil {
		return tErr
	}

	// Wait for reply
	select {
	case tErr = <-op.Result:
		if tErr.IsError() {
			return tErr
		}
		return nil
	case <-ctx.Done():
		return nil
	case <-time.After(1 * time.Minute):
		return terminal.ErrTimeout.With("timed out while waiting for sync crane result")
	}
}

func runSyncStateOp(t terminal.Terminal, opID uint32, data *container.Container) (terminal.Operation, *terminal.Error) {
	// Check if we are a on a crane controller.
	var ok bool
	var controller *CraneControllerTerminal
	if controller, ok = t.(*CraneControllerTerminal); !ok {
		return nil, terminal.ErrIncorrectUsage.With("can only be used with a crane controller")
	}

	// Check if we are a public Hub and whether the lane is public too.
	if !conf.PublicHub() || !controller.Crane.Public() {
		return nil, terminal.ErrPermissionDenied.With("only public lanes can sync crane status")
	}

	// Load message.
	syncState := &SyncStateMessage{}
	_, err := dsd.Load(data.CompileData(), syncState)
	if err != nil {
		return nil, terminal.ErrMalformedData.With("failed to load sync state message: %w", err)
	}

	// Apply optimization state.
	controller.Crane.NetState.lock.Lock()
	defer controller.Crane.NetState.lock.Unlock()
	controller.Crane.NetState.stoppingRequestedByPeer = syncState.RequestStopping

	// Apply crane state only when we don't own the crane.
	if !controller.Crane.IsMine() {
		// Apply sync state.
		var changed bool
		if syncState.Stopping {
			if controller.Crane.stopping.SetToIf(false, true) {
				controller.Crane.NetState.markedStoppingAt = time.Now()
				changed = true
			}
		} else {
			if controller.Crane.stopping.SetToIf(true, false) {
				controller.Crane.NetState.markedStoppingAt = time.Time{}
				changed = true
			}
		}

		// Notify of change.
		if changed {
			controller.Crane.NotifyUpdate()
		}
	}

	return nil, nil
}
