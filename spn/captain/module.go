package captain

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/safing/portbase/api"
	"github.com/safing/portbase/config"
	"github.com/safing/portbase/log"
	"github.com/safing/portbase/modules"
	"github.com/safing/portbase/modules/subsystems"
	"github.com/safing/portbase/rng"
	"github.com/safing/portmaster/service/network/netutils"
	"github.com/safing/portmaster/spn/conf"
	"github.com/safing/portmaster/spn/crew"
	"github.com/safing/portmaster/spn/navigator"
	"github.com/safing/portmaster/spn/patrol"
	"github.com/safing/portmaster/spn/ships"
	_ "github.com/safing/portmaster/spn/sluice"
)

const controlledFailureExitCode = 24

var module *modules.Module

// SPNConnectedEvent is the name of the event that is fired when the SPN has connected and is ready.
const SPNConnectedEvent = "spn connect"

func init() {
	module = modules.Register("captain", prep, start, stop, "base", "terminal", "cabin", "ships", "docks", "crew", "navigator", "sluice", "patrol", "netenv")
	module.RegisterEvent(SPNConnectedEvent, false)
	subsystems.Register(
		"spn",
		"SPN",
		"Safing Privacy Network",
		module,
		"config:spn/",
		&config.Option{
			Name:         "SPN Module",
			Key:          CfgOptionEnableSPNKey,
			Description:  "Start the Safing Privacy Network module. If turned off, the SPN is fully disabled on this device.",
			OptType:      config.OptTypeBool,
			DefaultValue: false,
			Annotations: config.Annotations{
				config.DisplayOrderAnnotation: cfgOptionEnableSPNOrder,
				config.CategoryAnnotation:     "General",
			},
		},
	)
}

func prep() error {
	// Check if we can parse the bootstrap hub flag.
	if err := prepBootstrapHubFlag(); err != nil {
		return err
	}

	// Register SPN status provider.
	if err := registerSPNStatusProvider(); err != nil {
		return err
	}

	// Register API endpoints.
	if err := registerAPIEndpoints(); err != nil {
		return err
	}

	if conf.PublicHub() {
		// Register API authenticator.
		if err := api.SetAuthenticator(apiAuthenticator); err != nil {
			return err
		}

		if err := module.RegisterEventHook(
			"patrol",
			patrol.ChangeSignalEventName,
			"trigger hub status maintenance",
			func(_ context.Context, _ any) error {
				TriggerHubStatusMaintenance()
				return nil
			},
		); err != nil {
			return err
		}
	}

	return prepConfig()
}

func start() error {
	maskingBytes, err := rng.Bytes(16)
	if err != nil {
		return fmt.Errorf("failed to get random bytes for masking: %w", err)
	}
	ships.EnableMasking(maskingBytes)

	// Initialize intel.
	if err := registerIntelUpdateHook(); err != nil {
		return err
	}
	if err := updateSPNIntel(module.Ctx, nil); err != nil {
		log.Errorf("spn/captain: failed to update SPN intel: %s", err)
	}

	// Initialize identity and piers.
	if conf.PublicHub() {
		// Load identity.
		if err := loadPublicIdentity(); err != nil {
			// We cannot recover from this, set controlled failure (do not retry).
			modules.SetExitStatusCode(controlledFailureExitCode)

			return err
		}

		// Check if any networks are configured.
		if !conf.HubHasIPv4() && !conf.HubHasIPv6() {
			// We cannot recover from this, set controlled failure (do not retry).
			modules.SetExitStatusCode(controlledFailureExitCode)

			return errors.New("no IP addresses for Hub configured (or detected)")
		}

		// Start management of identity and piers.
		if err := prepPublicIdentityMgmt(); err != nil {
			return err
		}
		// Set ID to display on http info page.
		ships.DisplayHubID = publicIdentity.ID
		// Start listeners.
		if err := startPiers(); err != nil {
			return err
		}

		// Enable connect operation.
		crew.EnableConnecting(publicIdentity.Hub)
	}

	// Subscribe to updates of cranes.
	startDockHooks()

	// bootstrapping
	if err := processBootstrapHubFlag(); err != nil {
		return err
	}
	if err := processBootstrapFileFlag(); err != nil {
		return err
	}

	// network optimizer
	if conf.PublicHub() {
		module.NewTask("optimize network", optimizeNetwork).
			Repeat(1 * time.Minute).
			Schedule(time.Now().Add(15 * time.Second))
	}

	// client + home hub manager
	if conf.Client() {
		module.StartServiceWorker("client manager", 0, clientManager)

		// Reset failing hubs when the network changes while not connected.
		if err := module.RegisterEventHook(
			"netenv",
			"network changed",
			"reset failing hubs",
			func(_ context.Context, _ interface{}) error {
				if ready.IsNotSet() {
					navigator.Main.ResetFailingStates(module.Ctx)
				}
				return nil
			},
		); err != nil {
			return err
		}
	}

	return nil
}

func stop() error {
	// Reset intel resource so that it is loaded again when starting.
	resetSPNIntel()

	// Unregister crane update hook.
	stopDockHooks()

	// Send shutdown status message.
	if conf.PublicHub() {
		publishShutdownStatus()
		stopPiers()
	}

	return nil
}

// apiAuthenticator grants User permissions for local API requests.
func apiAuthenticator(r *http.Request, s *http.Server) (*api.AuthToken, error) {
	// Get remote IP.
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to split host/port: %w", err)
	}
	remoteIP := net.ParseIP(host)
	if remoteIP == nil {
		return nil, fmt.Errorf("failed to parse remote address %s", host)
	}

	if !netutils.GetIPScope(remoteIP).IsLocalhost() {
		return nil, api.ErrAPIAccessDeniedMessage
	}

	return &api.AuthToken{
		Read:  api.PermitUser,
		Write: api.PermitUser,
	}, nil
}
