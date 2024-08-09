package captain

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/spn/conf"
	"github.com/safing/portmaster/spn/docks"
	"github.com/safing/portmaster/spn/hub"
	"github.com/safing/portmaster/spn/ships"
	"github.com/safing/portmaster/spn/terminal"
)

// EstablishCrane establishes a crane to another Hub.
func EstablishCrane(callerCtx context.Context, dst *hub.Hub) (*docks.Crane, error) {
	if conf.PublicHub() && dst.ID == publicIdentity.ID {
		return nil, errors.New("connecting to self")
	}
	if docks.GetAssignedCrane(dst.ID) != nil {
		return nil, fmt.Errorf("route to %s already exists", dst.ID)
	}

	ship, err := ships.Launch(callerCtx, dst, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to launch ship: %w", err)
	}

	// If not a public hub, mark all ships as public in order to show unmasked data in logs.
	if !conf.PublicHub() {
		ship.MarkPublic()
	}

	crane, err := docks.NewCrane(ship, dst, publicIdentity)
	if err != nil {
		return nil, fmt.Errorf("failed to create crane: %w", err)
	}

	err = crane.Start(callerCtx)
	if err != nil {
		return nil, fmt.Errorf("failed to start crane: %w", err)
	}

	// Start gossip op for live map updates.
	_, tErr := NewGossipOp(crane.Controller)
	if tErr != nil {
		crane.Stop(tErr)
		return nil, fmt.Errorf("failed to start gossip op: %w", tErr)
	}

	return crane, nil
}

// EstablishPublicLane establishes a crane to another Hub and publishes it.
func EstablishPublicLane(ctx context.Context, dst *hub.Hub) (*docks.Crane, *terminal.Error) {
	// Create new context with timeout.
	// The maximum timeout is a worst case safeguard.
	// Keep in mind that multiple IPs and protocols may be tried in all configurations.
	// Some servers will be (possibly on purpose) hard to reach.
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	// Connect to destination and establish communication.
	crane, err := EstablishCrane(ctx, dst)
	if err != nil {
		return nil, terminal.ErrInternalError.With("failed to establish crane: %w", err)
	}

	// Publish as Lane.
	publishOp, tErr := NewPublishOp(crane.Controller, publicIdentity)
	if tErr != nil {
		return nil, terminal.ErrInternalError.With("failed to publish: %w", err)
	}

	// Wait for publishing to complete.
	select {
	case tErr := <-publishOp.Result():
		if !tErr.Is(terminal.ErrExplicitAck) {
			// Stop crane again, because we failed to publish it.
			defer crane.Stop(nil)
			return nil, terminal.ErrInternalError.With("failed to publish lane: %w", tErr)
		}

	case <-crane.Controller.Ctx().Done():
		defer crane.Stop(nil)
		return nil, terminal.ErrStopping

	case <-ctx.Done():
		defer crane.Stop(nil)
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, terminal.ErrTimeout
		}
		return nil, terminal.ErrCanceled
	}

	// Query all gossip msgs.
	_, tErr = NewGossipQueryOp(crane.Controller)
	if tErr != nil {
		log.Warningf("spn/captain: failed to start initial gossip query: %s", tErr)
	}

	return crane, nil
}
