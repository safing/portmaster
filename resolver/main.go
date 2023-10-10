package resolver

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/tevino/abool"

	"github.com/safing/portbase/log"
	"github.com/safing/portbase/modules"
	"github.com/safing/portbase/notifications"
	"github.com/safing/portbase/utils/debug"
	_ "github.com/safing/portmaster/core/base"
	"github.com/safing/portmaster/intel"
	"github.com/safing/portmaster/netenv"
)

var module *modules.Module

func init() {
	module = modules.Register("resolver", prep, start, nil, "base", "netenv")
}

func prep() error {
	// Set DNS test connectivity function for the online status check
	netenv.DNSTestQueryFunc = func(ctx context.Context, fdqn string) (ips []net.IP, ok bool, err error) {
		return testConnectivity(ctx, fdqn, nil)
	}

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

	// Force resolvers to reconnect when SPN has connected.
	if err := module.RegisterEventHook(
		"captain",
		"spn connect", // Defined by captain.SPNConnectedEvent
		"force resolver reconnect",
		func(ctx context.Context, _ any) error {
			ForceResolverReconnect(ctx)
			return nil
		},
	); err != nil {
		// This module does not depend on the SPN/Captain module, and probably should not.
		log.Warningf("resolvers: failed to register event hook for captain/spn-connect: %s", err)
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

	// Check failing resolvers regularly and when the network changes.
	checkFailingResolversTask := module.NewTask("check failing resolvers", checkFailingResolvers).Repeat(1 * time.Minute)
	err = module.RegisterEventHook(
		"netenv",
		netenv.NetworkChangedEvent,
		"check failing resolvers",
		func(_ context.Context, _ any) error {
			checkFailingResolversTask.StartASAP()
			return nil
		},
	)
	if err != nil {
		return err
	}

	module.NewTask("suggest using stale cache", suggestUsingStaleCacheTask).Repeat(2 * time.Minute)

	module.StartServiceWorker(
		"mdns handler",
		5*time.Second,
		listenToMDNS,
	)

	module.StartServiceWorker("name record delayed cache writer", 0, recordDatabase.DelayedCacheWriter)
	module.StartServiceWorker("ip info delayed cache writer", 0, ipInfoDatabase.DelayedCacheWriter)

	return nil
}

var localAddrFactory func(network string) net.Addr

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

	failingResolverErrorID = "resolver:all-configured-resolvers-failed"
)

func notifyAboutFailingResolvers() {
	failingResolverNotificationLock.Lock()
	defer failingResolverNotificationLock.Unlock()
	failingResolverNotificationSet.Set()

	// Check if already set.
	if failingResolverNotification != nil {
		return
	}

	// Create new notification.
	n := &notifications.Notification{
		EventID: failingResolverErrorID,
		Type:    notifications.Error,
		Title:   "Configured DNS Servers Failing",
		Message: `All configured DNS servers in Portmaster are failing.

You might not be able to connect to these servers, or all of these servers are offline.  
Choosing different DNS servers might fix this problem.

While the issue persists, Portmaster will use the DNS servers from your system or network, if permitted by configuration.

Alternatively, there might be something on your device that is interfering with Portmaster. This could be a firewall or another secure DNS resolver software. If that is your suspicion, please [check if you are running incompatible software here](https://docs.safing.io/portmaster/install/status/software-compatibility).

This notification will go away when Portmaster detects a working configured DNS server.`,
		ShowOnSystem: true,
		AvailableActions: []*notifications.Action{{
			Text: "Change DNS Servers",
			Type: notifications.ActionTypeOpenSetting,
			Payload: &notifications.ActionTypeOpenSettingPayload{
				Key: CfgOptionNameServersKey,
			},
		}},
	}
	notifications.Notify(n)

	failingResolverNotification = n
	n.AttachToModule(module)
}

func resetFailingResolversNotification() {
	if failingResolverNotificationSet.IsNotSet() {
		return
	}

	failingResolverNotificationLock.Lock()
	defer failingResolverNotificationLock.Unlock()

	// Remove the notification.
	if failingResolverNotification != nil {
		failingResolverNotification.Delete()
		failingResolverNotification = nil
	}

	// Additionally, resolve the module error, if not done through the notification.
	module.Resolve(failingResolverErrorID)
}

// AddToDebugInfo adds the system status to the given debug.Info.
func AddToDebugInfo(di *debug.Info) {
	resolversLock.Lock()
	defer resolversLock.Unlock()

	content := make([]string, 0, (len(globalResolvers)*4)-1)
	var working, total int
	for i, resolver := range globalResolvers {
		// Count for summary.
		total++
		failing := resolver.Conn.IsFailing()
		if !failing {
			working++
		}

		// Add section.
		content = append(content, resolver.Info.DescriptiveName())
		content = append(content, fmt.Sprintf("  %s", resolver.Info.ID()))
		if resolver.SearchOnly {
			content = append(content, "  Used for search domains only!")
		}
		if len(resolver.Search) > 0 {
			content = append(content, fmt.Sprintf("  Search Domains: %v", strings.Join(resolver.Search, ", ")))
		}
		content = append(content, fmt.Sprintf("  Failing: %v", resolver.Conn.IsFailing()))

		// Add a empty line for all but the last entry.
		if i+1 < len(globalResolvers) {
			content = append(content, "")
		}
	}

	di.AddSection(
		fmt.Sprintf("Resolvers: %d/%d", working, total),
		debug.UseCodeSection|debug.AddContentLineBreaks,
		content...,
	)
}
