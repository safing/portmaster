package docks

import (
	"context"
	"fmt"
	"time"

	"github.com/safing/portmaster/spn/hub"
	"github.com/safing/portmaster/spn/ships"
	"github.com/safing/portmaster/spn/terminal"
)

// Measurement Configuration.
const (
	CraneMeasurementTTLDefault    = 30 * time.Minute
	CraneMeasurementTTLByCostBase = 1 * time.Minute
	CraneMeasurementTTLByCostMin  = 30 * time.Minute
	CraneMeasurementTTLByCostMax  = 3 * time.Hour

	// With a base TTL of 1m, this leads to:
	// 20c     -> 20m -> raised to 30m
	// 50c     -> 50m
	// 100c    -> 1h40m
	// 1000c   -> 16h40m -> capped to 3h.
)

// MeasureHub measures the connection to this Hub and saves the results to the
// Hub.
func MeasureHub(ctx context.Context, h *hub.Hub, checkExpiryWith time.Duration) *terminal.Error {
	// Check if we are measuring before building a connection.
	if capacityTestRunning.IsSet() {
		return terminal.ErrTryAgainLater.With("another capacity op is already running")
	}

	// Check if we have a connection to this Hub.
	crane := GetAssignedCrane(h.ID)
	if crane == nil {
		// Connect to Hub.
		var err error
		crane, err = establishCraneForMeasuring(ctx, h)
		if err != nil {
			return terminal.ErrConnectionError.With("failed to connect to %s: %s", h, err)
		}
		// Stop crane if established just for measuring.
		defer crane.Stop(nil)
	}

	// Run latency test.
	_, expires := h.GetMeasurements().GetLatency()
	if checkExpiryWith == 0 || time.Now().Add(-checkExpiryWith).After(expires) {
		latOp, tErr := NewLatencyTestOp(crane.Controller)
		if !tErr.IsOK() {
			return tErr
		}
		select {
		case tErr = <-latOp.Result():
			if !tErr.IsOK() {
				return tErr
			}
		case <-ctx.Done():
			return terminal.ErrCanceled
		case <-time.After(1 * time.Minute):
			crane.Controller.StopOperation(latOp, terminal.ErrTimeout)
			return terminal.ErrTimeout.With("waiting for latency test")
		}
	}

	// Run capacity test.
	_, expires = h.GetMeasurements().GetCapacity()
	if checkExpiryWith == 0 || time.Now().Add(-checkExpiryWith).After(expires) {
		capOp, tErr := NewCapacityTestOp(crane.Controller, nil)
		if !tErr.IsOK() {
			return tErr
		}
		select {
		case tErr = <-capOp.Result():
			if !tErr.IsOK() {
				return tErr
			}
		case <-ctx.Done():
			return terminal.ErrCanceled
		case <-time.After(1 * time.Minute):
			crane.Controller.StopOperation(capOp, terminal.ErrTimeout)
			return terminal.ErrTimeout.With("waiting for capacity test")
		}
	}

	return nil
}

func establishCraneForMeasuring(ctx context.Context, dst *hub.Hub) (*Crane, error) {
	ship, err := ships.Launch(ctx, dst, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to launch ship: %w", err)
	}

	crane, err := NewCrane(ship, dst, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create crane: %w", err)
	}

	err = crane.Start(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to start crane: %w", err)
	}

	return crane, nil
}
