package docks

import (
	"time"

	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/service/mgr"
	"github.com/safing/portmaster/spn/terminal"
	"github.com/safing/structures/container"
)

const (
	defaultTerminalIdleTimeout = 15 * time.Minute
	remoteTerminalIdleTimeout  = 30 * time.Minute
)

// EstablishNewTerminal establishes a new terminal with the crane.
func (crane *Crane) EstablishNewTerminal(
	localTerm terminal.Terminal,
	initData *container.Container,
) *terminal.Error {
	// Create message.
	msg := terminal.NewEmptyMsg()
	msg.FlowID = localTerm.ID()
	msg.Type = terminal.MsgTypeInit
	msg.Data = initData

	// Register terminal with crane.
	crane.setTerminal(localTerm)

	// Send message.
	select {
	case crane.controllerMsgs <- msg:
		log.Debugf("spn/docks: %s initiated new terminal %d", crane, localTerm.ID())
		return nil
	case <-crane.ctx.Done():
		crane.AbandonTerminal(localTerm.ID(), terminal.ErrStopping.With("initiation aborted"))
		return terminal.ErrStopping
	}
}

func (crane *Crane) establishTerminal(id uint32, initData *container.Container) {
	// Create new remote crane terminal.
	newTerminal, _, err := NewRemoteCraneTerminal(
		crane,
		id,
		initData,
	)
	if err == nil {
		// Connections via public cranes have a timeout.
		if crane.Public() {
			newTerminal.TerminalBase.SetTimeout(remoteTerminalIdleTimeout)
		}
		// Register terminal with crane.
		crane.setTerminal(newTerminal)
		log.Debugf("spn/docks: %s established new crane terminal %d", crane, newTerminal.ID())
		return
	}

	// If something goes wrong, send an error back.
	log.Warningf("spn/docks: %s failed to establish crane terminal: %s", crane, err)

	// Build abandon message.
	msg := terminal.NewMsg(err.Pack())
	msg.FlowID = id
	msg.Type = terminal.MsgTypeStop

	// Send message directly, or async.
	select {
	case crane.terminalMsgs <- msg:
	default:
		// Send error async.
		module.mgr.Go("abandon terminal", func(ctx *mgr.WorkerCtx) error {
			select {
			case crane.terminalMsgs <- msg:
			case <-ctx.Done():
			}
			return nil
		})
	}
}
