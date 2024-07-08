package captain

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/safing/portmaster/base/api"
	"github.com/safing/portmaster/base/config"
	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/base/rng"
	"github.com/safing/portmaster/service/mgr"
	"github.com/safing/portmaster/service/netenv"
	"github.com/safing/portmaster/service/network/netutils"
	"github.com/safing/portmaster/service/updates"
	"github.com/safing/portmaster/spn/conf"
	"github.com/safing/portmaster/spn/crew"
	"github.com/safing/portmaster/spn/navigator"
	"github.com/safing/portmaster/spn/patrol"
	"github.com/safing/portmaster/spn/ships"
	_ "github.com/safing/portmaster/spn/sluice"
)

const controlledFailureExitCode = 24

// SPNConnectedEvent is the name of the event that is fired when the SPN has connected and is ready.
const SPNConnectedEvent = "spn connect"

type Captain struct {
	mgr      *mgr.Manager
	instance instance

	shutdownFunc func(exitCode int)

	healthCheckTicker    *mgr.SleepyTicker
	maintainPublicStatus *mgr.WorkerMgr

	States            *mgr.StateMgr
	EventSPNConnected *mgr.EventMgr[struct{}]
}

func (c *Captain) Manager() *mgr.Manager {
	return c.mgr
}

func (c *Captain) Start() error {
	if err := prep(); err != nil {
		return err
	}

	return start()
}

func (c *Captain) Stop() error {
	return stop()
}

func (c *Captain) SetSleep(enabled bool) {
	if c.healthCheckTicker != nil {
		c.healthCheckTicker.SetSleep(enabled)
	}
}

func init() {
	// subsystems.Register(
	// 	"spn",
	// 	"SPN",
	// 	"Safing Privacy Network",
	// 	module,
	// 	"config:spn/",
	// 	&config.Option{
	// 		Name:         "SPN Module",
	// 		Key:          CfgOptionEnableSPNKey,
	// 		Description:  "Start the Safing Privacy Network module. If turned off, the SPN is fully disabled on this device.",
	// 		OptType:      config.OptTypeBool,
	// 		DefaultValue: false,
	// 		Annotations: config.Annotations{
	// 			config.DisplayOrderAnnotation: cfgOptionEnableSPNOrder,
	// 			config.CategoryAnnotation:     "General",
	// 		},
	// 	},
	// )
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

		module.instance.Patrol().EventChangeSignal.AddCallback(
			"trigger hub status maintenance",
			func(_ *mgr.WorkerCtx, _ struct{}) (bool, error) {
				TriggerHubStatusMaintenance()
				return false, nil
			},
		)
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
	module.mgr.Go("start", func(wc *mgr.WorkerCtx) error {
		if err := registerIntelUpdateHook(); err != nil {
			return err
		}
		if err := updateSPNIntel(module.mgr.Ctx(), nil); err != nil {
			log.Errorf("spn/captain: failed to update SPN intel: %s", err)
		}

		// Initialize identity and piers.
		if conf.PublicHub() {
			// Load identity.
			if err := loadPublicIdentity(); err != nil {
				// We cannot recover from this, set controlled failure (do not retry).
				module.shutdownFunc(controlledFailureExitCode)

				return err
			}

			// Check if any networks are configured.
			if !conf.HubHasIPv4() && !conf.HubHasIPv6() {
				// We cannot recover from this, set controlled failure (do not retry).
				module.shutdownFunc(controlledFailureExitCode)

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
			module.mgr.Delay("optimize network delay", 15*time.Second, optimizeNetwork).Repeat(1 * time.Minute)
		}

		// client + home hub manager
		if conf.Client() {
			module.mgr.Go("client manager", clientManager)

			// Reset failing hubs when the network changes while not connected.
			module.instance.NetEnv().EventNetworkChange.AddCallback("reset failing hubs", func(_ *mgr.WorkerCtx, _ struct{}) (bool, error) {
				if ready.IsNotSet() {
					navigator.Main.ResetFailingStates(module.mgr.Ctx())
				}
				return false, nil
			})
		}
		return nil
	})
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

var (
	module     *Captain
	shimLoaded atomic.Bool
)

// New returns a new Captain module.
func New(instance instance, shutdownFunc func(exitCode int)) (*Captain, error) {
	if !shimLoaded.CompareAndSwap(false, true) {
		return nil, errors.New("only one instance allowed")
	}
	m := mgr.New("Captain")
	module = &Captain{
		mgr:          m,
		instance:     instance,
		shutdownFunc: shutdownFunc,

		States:               mgr.NewStateMgr(m),
		EventSPNConnected:    mgr.NewEventMgr[struct{}](SPNConnectedEvent, m),
		maintainPublicStatus: m.NewWorkerMgr("maintain public status", maintainPublicStatus, nil),
	}
	return module, nil
}

type instance interface {
	NetEnv() *netenv.NetEnv
	Patrol() *patrol.Patrol
	Config() *config.Config
	Updates() *updates.Updates
}
