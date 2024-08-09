package docks

import (
	"context"
	"fmt"
	"net"
	"sync"

	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/spn/conf"
	"github.com/safing/portmaster/spn/hub"
	"github.com/safing/portmaster/spn/ships"
	"github.com/safing/portmaster/spn/terminal"
)

var hubImportLock sync.Mutex

// ImportAndVerifyHubInfo imports the given hub message and verifies them.
func ImportAndVerifyHubInfo(ctx context.Context, hubID string, announcementData, statusData []byte, mapName string, scope hub.Scope) (h *hub.Hub, forward bool, tErr *terminal.Error) {
	var firstErr *terminal.Error

	// Synchronize import, as we might easily learn of a new hub from different
	// gossip channels simultaneously.
	hubImportLock.Lock()
	defer hubImportLock.Unlock()

	// Check arguments.
	if announcementData == nil && statusData == nil {
		return nil, false, terminal.ErrInternalError.With("no announcement or status supplied")
	}

	// Import Announcement, if given.
	var hubKnown, hubChanged bool
	if announcementData != nil {
		hubFromMsg, known, changed, err := hub.ApplyAnnouncement(nil, announcementData, mapName, scope, false)
		if err != nil {
			firstErr = terminal.ErrInternalError.With("failed to apply announcement: %w", err)
		}
		if known {
			hubKnown = true
		}
		if changed {
			hubChanged = true
		}
		if hubFromMsg != nil {
			h = hubFromMsg
		}
	}

	// Import Status, if given.
	if statusData != nil {
		hubFromMsg, known, changed, err := hub.ApplyStatus(h, statusData, mapName, scope, false)
		if err != nil && firstErr == nil {
			firstErr = terminal.ErrInternalError.With("failed to apply status: %w", err)
		}
		if known && announcementData == nil {
			// If we parsed an announcement before, "known" will always be true here,
			// as we supply hub.ApplyStatus with a hub.
			hubKnown = true
		}
		if changed {
			hubChanged = true
		}
		if hubFromMsg != nil {
			h = hubFromMsg
		}
	}

	// Only continue if we now have a Hub.
	if h == nil {
		if firstErr != nil {
			return nil, false, firstErr
		}
		return nil, false, terminal.ErrInternalError.With("got not hub after data import")
	}

	// Abort if the given hub ID does not match.
	// We may have just connected to the wrong IP address.
	if hubID != "" && h.ID != hubID {
		return nil, false, terminal.ErrInternalError.With("hub mismatch")
	}

	// Verify hub if:
	// - There is no error up until here.
	// - There has been any change.
	// - The hub is not verified yet.
	// - We're a public Hub.
	// - We're not testing.
	if firstErr == nil && hubChanged && !h.Verified() && conf.PublicHub() && !runningTests {
		if !conf.HubHasIPv4() && !conf.HubHasIPv6() {
			firstErr = terminal.ErrInternalError.With("no hub networks set")
		}
		if h.Info.IPv4 != nil && conf.HubHasIPv4() {
			err := verifyHubIP(ctx, h, h.Info.IPv4)
			if err != nil {
				firstErr = terminal.ErrIntegrity.With("failed to verify IPv4 address %s of %s: %w", h.Info.IPv4, h, err)
			}
		}
		if h.Info.IPv6 != nil && conf.HubHasIPv6() {
			err := verifyHubIP(ctx, h, h.Info.IPv6)
			if err != nil {
				firstErr = terminal.ErrIntegrity.With("failed to verify IPv6 address %s of %s: %w", h.Info.IPv6, h, err)
			}
		}

		if firstErr != nil {
			func() {
				h.Lock()
				defer h.Unlock()
				h.InvalidInfo = true
			}()
			log.Warningf("spn/docks: failed to verify IPs of %s: %s", h, firstErr)
		} else {
			func() {
				h.Lock()
				defer h.Unlock()
				h.VerifiedIPs = true
			}()
			log.Infof("spn/docks: verified IPs of %s: IPv4=%s IPv6=%s", h, h.Info.IPv4, h.Info.IPv6)
		}
	}

	// Dismiss initial imports with errors.
	if !hubKnown && firstErr != nil {
		return nil, false, firstErr
	}

	// Don't do anything if nothing changed.
	if !hubChanged {
		return h, false, firstErr
	}

	// We now have one of:
	// - A unknown Hub without error.
	// - A known Hub without error.
	// - A known Hub with error, which we want to save and propagate.

	// Save the Hub to the database.
	err := h.Save()
	if err != nil {
		log.Errorf("spn/docks: failed to persist %s: %s", h, err)
	}

	// Save the raw messages to the database.
	if announcementData != nil {
		err = hub.SaveHubMsg(h.ID, h.Map, hub.MsgTypeAnnouncement, announcementData)
		if err != nil {
			log.Errorf("spn/docks: failed to save raw announcement msg of %s: %s", h, err)
		}
	}
	if statusData != nil {
		err = hub.SaveHubMsg(h.ID, h.Map, hub.MsgTypeStatus, statusData)
		if err != nil {
			log.Errorf("spn/docks: failed to save raw status msg of %s: %s", h, err)
		}
	}

	return h, true, firstErr
}

func verifyHubIP(ctx context.Context, h *hub.Hub, ip net.IP) error {
	// Create connection.
	ship, err := ships.Launch(ctx, h, nil, ip)
	if err != nil {
		return fmt.Errorf("failed to launch ship to %s: %w", ip, err)
	}

	// Start crane for receiving reply.
	crane, err := NewCrane(ship, h, nil)
	if err != nil {
		return fmt.Errorf("failed to create crane: %w", err)
	}
	module.mgr.Go("crane unloader", crane.unloader)
	defer crane.Stop(nil)

	// Verify Hub.
	err = crane.VerifyConnectedHub(ctx)
	if err != nil {
		return err
	}

	// End connection.
	tErr := crane.endInit()
	if tErr != nil {
		log.Debugf("spn/docks: failed to end verification connection to %s: %s", ip, tErr)
	}

	return nil
}
