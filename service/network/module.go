package network

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/service/firewall/interception/dnsmonitor"
	"github.com/safing/portmaster/service/mgr"
	"github.com/safing/portmaster/service/netenv"
	"github.com/safing/portmaster/service/network/state"
	"github.com/safing/portmaster/service/profile"
	"github.com/safing/portmaster/service/resolver"
)

// Events.
const (
	ConnectionReattributedEvent = "connection re-attributed"
)

type Network struct {
	mgr      *mgr.Manager
	instance instance

	dnsRequestTicker        *mgr.SleepyTicker
	connectionCleanerTicker *mgr.SleepyTicker

	EventConnectionReattributed *mgr.EventMgr[string]
}

func (n *Network) Manager() *mgr.Manager {
	return n.mgr
}

func (n *Network) Start() error {
	return start()
}

func (n *Network) Stop() error {
	return nil
}

func (n *Network) SetSleep(enabled bool) {
	if n.dnsRequestTicker != nil {
		n.dnsRequestTicker.SetSleep(enabled)
	}
	if n.connectionCleanerTicker != nil {
		n.connectionCleanerTicker.SetSleep(enabled)
	}
}

var defaultFirewallHandler FirewallHandler

// SetDefaultFirewallHandler sets the default firewall handler.
func SetDefaultFirewallHandler(handler FirewallHandler) {
	if defaultFirewallHandler == nil {
		defaultFirewallHandler = handler
	}
}

func prep() error {
	if netenv.IPv6Enabled() {
		state.EnableTCPDualStack()
		state.EnableUDPDualStack()
	}

	return registerAPIEndpoints()
}

func start() error {
	err := registerAsDatabase()
	if err != nil {
		return err
	}

	if err := registerMetrics(); err != nil {
		return err
	}

	module.mgr.Go("clean connections", connectionCleaner)
	module.mgr.Go("write open dns requests", openDNSRequestWriter)
	module.instance.Profile().EventDelete.AddCallback("re-attribute connections from deleted profile", reAttributeConnections)

	return nil
}

var reAttributionLock sync.Mutex

// reAttributeConnections finds all connections of a deleted profile and re-attributes them.
// Expected event data: scoped profile ID.
func reAttributeConnections(_ *mgr.WorkerCtx, profileID string) (bool, error) {
	profileSource, profileID, ok := strings.Cut(profileID, "/")
	if !ok {
		return false, fmt.Errorf("event data does not seem to be a scoped profile ID: %v", profileID)
	}

	// Hold a lock for re-attribution, to prevent simultaneous processing of the
	// same connections and make logging cleaner.
	reAttributionLock.Lock()
	defer reAttributionLock.Unlock()

	// Create tracing context.
	ctx, tracer := log.AddTracer(context.Background())
	defer tracer.Submit()
	tracer.Infof("network: re-attributing connections from deleted profile %s/%s", profileSource, profileID)

	// Count and log how many connections were re-attributed.
	var reAttributed int

	// Re-attribute connections.
	for _, conn := range conns.clone() {
		if reAttributeConnection(ctx, conn, profileID, profileSource) {
			reAttributed++
			tracer.Debugf("filter: re-attributed %s to %s", conn, conn.process.PrimaryProfileID)
		}
	}

	// Re-attribute dns connections.
	for _, conn := range dnsConns.clone() {
		if reAttributeConnection(ctx, conn, profileID, profileSource) {
			reAttributed++
			tracer.Debugf("filter: re-attributed %s to %s", conn, conn.process.PrimaryProfileID)
		}
	}

	tracer.Infof("filter: re-attributed %d connections", reAttributed)
	return false, nil
}

func reAttributeConnection(ctx context.Context, conn *Connection, profileID, profileSource string) (reAttributed bool) {
	// Lock the connection before checking anything to avoid a race condition with connection data collection.
	conn.Lock()
	defer conn.Unlock()

	// Check if the connection has the profile we are looking for.
	switch {
	case !conn.DataIsComplete():
		return false
	case conn.ProcessContext.Profile != profileID:
		return false
	case conn.ProcessContext.Source != profileSource:
		return false
	}

	// Attempt to assign new profile.
	err := conn.process.RefetchProfile(ctx)
	if err != nil {
		log.Tracer(ctx).Warningf("network: failed to refetch profile for %s: %s", conn, err)
		return false
	}

	// Set the new process context.
	conn.ProcessContext = getProcessContext(ctx, conn.process)
	conn.Save()

	// Trigger event for re-attribution.
	module.EventConnectionReattributed.Submit(conn.ID)

	log.Tracer(ctx).Debugf("filter: re-attributed %s to %s", conn, conn.process.PrimaryProfileID)
	return true
}

var (
	module     *Network
	shimLoaded atomic.Bool
)

// New returns a new Network module.
func New(instance instance) (*Network, error) {
	if !shimLoaded.CompareAndSwap(false, true) {
		return nil, errors.New("only one instance allowed")
	}
	m := mgr.New("Network")
	module = &Network{
		mgr:                         m,
		instance:                    instance,
		EventConnectionReattributed: mgr.NewEventMgr[string](ConnectionReattributedEvent, m),
	}

	if err := prep(); err != nil {
		return nil, err
	}

	return module, nil
}

type instance interface {
	Profile() *profile.ProfileModule
	Resolver() *resolver.ResolverModule
	DNSMonitor() *dnsmonitor.DNSMonitor
}
