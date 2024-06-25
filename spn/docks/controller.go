package docks

import (
	"github.com/safing/portmaster/spn/terminal"
	"github.com/safing/structures/container"
)

// CraneControllerTerminal is a terminal for the crane itself.
type CraneControllerTerminal struct {
	*terminal.TerminalBase

	Crane *Crane
}

// NewLocalCraneControllerTerminal returns a new local crane controller.
func NewLocalCraneControllerTerminal(
	crane *Crane,
	initMsg *terminal.TerminalOpts,
) (*CraneControllerTerminal, *container.Container, *terminal.Error) {
	// Remove unnecessary options from the crane controller.
	initMsg.Padding = 0

	// Create Terminal Base.
	t, initData, err := terminal.NewLocalBaseTerminal(
		crane.ctx,
		0,
		crane.ID,
		nil,
		initMsg,
		terminal.UpstreamSendFunc(crane.sendImportantTerminalMsg),
	)
	if err != nil {
		return nil, nil, err
	}

	return initCraneController(crane, t, initMsg), initData, nil
}

// NewRemoteCraneControllerTerminal returns a new remote crane controller.
func NewRemoteCraneControllerTerminal(
	crane *Crane,
	initData *container.Container,
) (*CraneControllerTerminal, *terminal.TerminalOpts, *terminal.Error) {
	// Create Terminal Base.
	t, initMsg, err := terminal.NewRemoteBaseTerminal(
		crane.ctx,
		0,
		crane.ID,
		nil,
		initData,
		terminal.UpstreamSendFunc(crane.sendImportantTerminalMsg),
	)
	if err != nil {
		return nil, nil, err
	}

	return initCraneController(crane, t, initMsg), initMsg, nil
}

func initCraneController(
	crane *Crane,
	t *terminal.TerminalBase,
	initMsg *terminal.TerminalOpts,
) *CraneControllerTerminal {
	// Create Crane Terminal and assign it as the extended Terminal.
	cct := &CraneControllerTerminal{
		TerminalBase: t,
		Crane:        crane,
	}
	t.SetTerminalExtension(cct)

	// Assign controller to crane.
	crane.Controller = cct
	crane.terminals[cct.ID()] = cct

	// Copy the options to the crane itself.
	crane.opts = *initMsg

	// Grant crane controller permission.
	t.GrantPermission(terminal.IsCraneController)

	// Start workers.
	t.StartWorkers(module.mgr, "crane controller terminal")

	return cct
}

// HandleAbandon gives the terminal the ability to cleanly shut down.
func (controller *CraneControllerTerminal) HandleAbandon(err *terminal.Error) (errorToSend *terminal.Error) {
	// Abandon terminal.
	controller.Crane.AbandonTerminal(0, err)

	return err
}

// HandleDestruction gives the terminal the ability to clean up.
func (controller *CraneControllerTerminal) HandleDestruction(err *terminal.Error) {
	// Stop controlled crane.
	controller.Crane.Stop(nil)
}
