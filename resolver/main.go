package resolver

import (
	"context"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/safing/portbase/log"
	"github.com/safing/portbase/modules"
	"github.com/safing/portbase/notifications"
	"github.com/safing/portmaster/intel"
	"github.com/tevino/abool"

	// module dependencies
	_ "github.com/safing/portmaster/core/base"
)

var (
	module *modules.Module
)

func init() {
	module = modules.Register("resolver", prep, start, nil, "base", "netenv")
}

func prep() error {
	intel.SetReverseResolver(ResolveIPAndValidate)

	if err := registerAPI(); err != nil {
		return err
	}

	if err := prepEnvResolver(); err != nil {
		return err
	}

	return prepConfig()
}

func start() error {
	// load resolvers from config and environment
	loadResolvers()

	// reload after network change
	err := module.RegisterEventHook(
		"netenv",
		"network changed",
		"update nameservers",
		func(_ context.Context, _ interface{}) error {
			loadResolvers()
			log.Debug("resolver: reloaded nameservers due to network change")
			return nil
		},
	)
	if err != nil {
		return err
	}

	// reload after config change
	prevNameservers := strings.Join(configuredNameServers(), " ")
	err = module.RegisterEventHook(
		"config",
		"config change",
		"update nameservers",
		func(_ context.Context, _ interface{}) error {
			newNameservers := strings.Join(configuredNameServers(), " ")
			if newNameservers != prevNameservers {
				prevNameservers = newNameservers

				loadResolvers()
				log.Debug("resolver: reloaded nameservers due to config change")
			}
			return nil
		},
	)
	if err != nil {
		return err
	}

	module.StartServiceWorker(
		"mdns handler",
		5*time.Second,
		listenToMDNS,
	)

	module.StartServiceWorker("name record delayed cache writer", 0, recordDatabase.DelayedCacheWriter)
	module.StartServiceWorker("ip info delayed cache writer", 0, ipInfoDatabase.DelayedCacheWriter)

	return nil
}

var (
	localAddrFactory func(network string) net.Addr
)

// SetLocalAddrFactory supplies the intel package with a function to get permitted local addresses for connections.
func SetLocalAddrFactory(laf func(network string) net.Addr) {
	if localAddrFactory == nil {
		localAddrFactory = laf
	}
}

func getLocalAddr(network string) net.Addr {
	if localAddrFactory != nil {
		return localAddrFactory(network)
	}
	return nil
}

var (
	failingResolverNotification     *notifications.Notification
	failingResolverNotificationSet  = abool.New()
	failingResolverNotificationLock sync.Mutex
)

func notifyAboutFailingResolvers(err error) {
	failingResolverNotificationLock.Lock()
	defer failingResolverNotificationLock.Unlock()
	failingResolverNotificationSet.Set()

	// Check if already set.
	if failingResolverNotification != nil {
		return
	}

	// Create new notification.
	n := &notifications.Notification{
		EventID:      "resolver:all-configured-resolvers-failed",
		Type:         notifications.Error,
		Title:        "Detected DNS Compatibility Issue",
		Message:      "Portmaster detected that something is interfering with its Secure DNS resolver. This could be a firewall or another secure DNS resolver software. Please check if you are running incompatible [software](https://docs.safing.io/portmaster/install/status/software-compatibility). Otherwise, please report the issue via [GitHub](https://github.com/safing/portmaster/issues) or send a mail to [support@safing.io](mailto:support@safing.io) so we can help you out.",
		ShowOnSystem: true,
	}
	notifications.Notify(n)

	failingResolverNotification = n
	n.AttachToModule(module)

	// Report the raw error as module error.
	module.NewErrorMessage("resolving", err).Report()
}

func resetFailingResolversNotification() {
	if failingResolverNotificationSet.IsNotSet() {
		return
	}

	failingResolverNotificationLock.Lock()
	defer failingResolverNotificationLock.Unlock()

	if failingResolverNotification != nil {
		failingResolverNotification.Delete()
		failingResolverNotification = nil
	}
}
