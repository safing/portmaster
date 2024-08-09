package docks

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/safing/portmaster/spn/cabin"
	"github.com/safing/portmaster/spn/terminal"
	"github.com/safing/structures/container"
	"github.com/safing/structures/varint"
)

const (
	hubVerificationPurpose = "hub identify verification"
)

// VerifyConnectedHub verifies the connected Hub.
func (crane *Crane) VerifyConnectedHub(callerCtx context.Context) error {
	if !crane.ship.IsMine() || crane.nextTerminalID != 0 || crane.Public() {
		return errors.New("hub verification can only be executed in init phase by the client")
	}

	// Create verification request.
	v, request, err := cabin.CreateVerificationRequest(hubVerificationPurpose, "", "")
	if err != nil {
		return fmt.Errorf("failed to create verification request: %w", err)
	}

	// Send it.
	msg := container.New(
		varint.Pack8(CraneMsgTypeVerify),
		request,
	)
	msg.PrependLength()
	err = crane.ship.Load(msg.CompileData())
	if err != nil {
		return terminal.ErrShipSunk.With("failed to send verification request: %w", err)
	}

	// Wait for reply.
	var reply *container.Container
	select {
	case reply = <-crane.unloading:
	case <-time.After(2 * time.Minute):
		// Use a big timeout here, as this might keep servers from joining the
		// network at all, as every servers needs to verify every server, no
		// matter how far away.
		return terminal.ErrTimeout.With("waiting for verification reply")
	case <-crane.ctx.Done():
		return terminal.ErrShipSunk.With("waiting for verification reply")
	case <-callerCtx.Done():
		return terminal.ErrShipSunk.With("waiting for verification reply")
	}

	// Verify reply.
	return v.Verify(reply.CompileData(), crane.ConnectedHub)
}

func (crane *Crane) handleCraneVerification(request *container.Container) *terminal.Error {
	// Check if we have an identity.
	if crane.identity == nil {
		return terminal.ErrIncorrectUsage.With("cannot handle verification request without designated identity")
	}

	response, err := crane.identity.SignVerificationRequest(
		request.CompileData(),
		hubVerificationPurpose,
		"", "",
	)
	if err != nil {
		return terminal.ErrInternalError.With("failed to sign verification request: %w", err)
	}
	msg := container.New(response)

	// Manually send reply.
	msg.PrependLength()
	err = crane.ship.Load(msg.CompileData())
	if err != nil {
		return terminal.ErrShipSunk.With("failed to send verification reply: %w", err)
	}

	return nil
}
