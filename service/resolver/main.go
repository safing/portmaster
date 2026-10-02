package resolver

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/tevino/abool"

	"github.com/safing/portmaster/base/config"
	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/base/notifications"
	"github.com/safing/portmaster/base/utils/debug"
	_ "github.com/safing/portmaster/service/core/base"
	"github.com/safing/portmaster/service/intel"
	"github.com/safing/portmaster/service/mgr"
	"github.com/safing/portmaster/service/netenv"
)

// ResolverModule is the DNS resolver module.
type ResolverModule struct { //nolint
	mgr      *mgr.Manager
	instance instance

	failingResolverWorkerMgr   *mgr.WorkerMgr
	suggestUsingStaleCacheTask *mgr.WorkerMgr

	isDisabled atomic.Bool

	states *mgr.StateMgr
}

// Manager returns the module manager.
func (rm *ResolverModule) Manager() *mgr.Manager {
	return rm.mgr
}

// States returns the module state manager.
func (rm *ResolverModule) States() *mgr.StateMgr {
	return rm.states
}

// Start starts the module.
func (rm *ResolverModule) Start() error {
	return start()
}

// Stop stops the module.
func (rm *ResolverModule) Stop() error {
	return nil
}

func (rm *ResolverModule) IsDisabled() bool {
	return rm.isDisabled.Load()
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
	module.instance.NetEnv().EventNetworkChange.AddCallback(
		"update nameservers",
		func(_ *mgr.WorkerCtx, _ struct{}) (bool, error) {
			loadResolvers()
			log.Debug("resolver: reloaded nameservers due to network change")
			return false, nil
		},
	)

	// Force resolvers to reconnect when SPN has connected.
	module.instance.GetEventSPNConnected().AddCallback(
		"force resolver reconnect",
		func(ctx *mgr.WorkerCtx, _ struct{}) (bool, error) {
			ForceResolverReconnect(ctx.Ctx())
			return false, nil
		})

	// reload after config change
	prevNameservers := strings.Join(configuredNameServers(), " ")
	module.instance.Config().EventConfigChange.AddCallback(
		"update nameservers",
		func(_ *mgr.WorkerCtx, _ struct{}) (bool, error) {
			newNameservers := strings.Join(configuredNameServers(), " ")
			if newNameservers != prevNameservers {
				prevNameservers = newNameservers

				loadResolvers()
				log.Debug("resolver: reloaded nameservers due to config change")
			}
			return false, nil
		})

	// Check failing resolvers regularly and when the network changes.
	module.failingResolverWorkerMgr = module.mgr.NewWorkerMgr("check failing resolvers", checkFailingResolvers, nil)
	module.failingResolverWorkerMgr.Go()
	module.instance.NetEnv().EventNetworkChange.AddCallback(
		"check failing resolvers",
		func(wc *mgr.WorkerCtx, _ struct{}) (bool, error) {
			return false, checkFailingResolvers(wc)
		})

	module.suggestUsingStaleCacheTask = module.mgr.NewWorkerMgr("suggest using stale cache", suggestUsingStaleCacheTask, nil)
	module.suggestUsingStaleCacheTask.Go()

	module.mgr.Go(
		"mdns handler",
		listenToMDNS,
	)

	module.mgr.Go("name record delayed cache writer", recordDatabase.DelayedCacheWriter)
	module.mgr.Go("ip info delayed cache writer", ipInfoDatabase.DelayedCacheWriter)

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
		Title:   "已配置的 DNS 服务器均失效",
		Message: `Portmaster 中所有已配置的 DNS 服务器均无法正常工作。

您可能无法连接到这些服务器，或者这些服务器全部处于离线状态。  
选择其他 DNS 服务器可能会解决此问题。

在问题持续期间，如果配置允许，Portmaster 将使用系统或网络中的 DNS 服务器。

另外，您的设备上也可能有某些程序正在干扰 Portmaster，例如防火墙或其他安全 DNS 解析软件。如果您有此怀疑，请[在此检查您是否运行了不兼容的软件](https://docs.safing.io/portmaster/install/status/software-compatibility)。

当 Portmaster 检测到可用的已配置 DNS 服务器时，此通知将自动消失。`,
		ShowOnSystem: true,
		AvailableActions: []*notifications.Action{{
			Text: "更改 DNS 服务器",
			Type: notifications.ActionTypeOpenSetting,
			Payload: &notifications.ActionTypeOpenSettingPayload{
				Key: CfgOptionNameServersKey,
			},
		}},
	}
	notifications.Notify(n)

	failingResolverNotification = n
	n.SyncWithState(module.states)
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
	module.states.Remove(failingResolverErrorID)
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
		if resolver.LinkLocalUnavailable {
			content = append(content, "  Link-local, but not available: ignoring")
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

var (
	module     *ResolverModule
	shimLoaded atomic.Bool
)

// New returns a new Resolver module.
func New(instance instance) (*ResolverModule, error) {
	if !shimLoaded.CompareAndSwap(false, true) {
		return nil, errors.New("only one instance allowed")
	}
	m := mgr.New("Resolver")
	module = &ResolverModule{
		mgr:      m,
		instance: instance,

		states: mgr.NewStateMgr(m),
	}
	if err := prep(); err != nil {
		return nil, err
	}
	return module, nil
}

type instance interface {
	NetEnv() *netenv.NetEnv
	Config() *config.Config
	GetEventSPNConnected() *mgr.EventMgr[struct{}]
}
