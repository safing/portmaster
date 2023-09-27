package network

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/safing/portbase/log"
	"github.com/safing/portbase/modules"
	"github.com/safing/portmaster/netenv"
	"github.com/safing/portmaster/network/state"
	"github.com/safing/portmaster/profile"
)

var (
	module *modules.Module

	defaultFirewallHandler FirewallHandler
)

// Events.
var (
	ConnectionReattributedEvent = "connection re-attributed"
)

func init() {
	module = modules.Register("network", prep, start, nil, "base", "netenv", "processes")
	module.RegisterEvent(ConnectionReattributedEvent, false)
}

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

	module.StartServiceWorker("clean connections", 0, connectionCleaner)
	module.StartServiceWorker("write open dns requests", 0, openDNSRequestWriter)

	if err := module.RegisterEventHook(
		"profiles",
		profile.DeletedEvent,
		"re-attribute connections from deleted profile",
		reAttributeConnections,
	); err != nil {
		return err
	}

	return nil
}

var reAttributionLock sync.Mutex

// reAttributeConnections finds all connections of a deleted profile and re-attributes them.
// Expected event data: scoped profile ID.
func reAttributeConnections(_ context.Context, eventData any) error {
	profileID, ok := eventData.(string)
	if !ok {
		return fmt.Errorf("event data is not a string: %v", eventData)
	}
	profileSource, profileID, ok := strings.Cut(profileID, "/")
	if !ok {
		return fmt.Errorf("event data does not seem to be a scoped profile ID: %v", eventData)
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
		// Check if connection is complete and attributed to the deleted profile.
		if conn.DataIsComplete() &&
			conn.ProcessContext.Profile == profileID &&
			conn.ProcessContext.Source == profileSource {

			reAttributeConnection(ctx, conn)
			reAttributed++
			tracer.Debugf("filter: re-attributed %s to %s", conn, conn.process.PrimaryProfileID)
		}
	}

	// Re-attribute dns connections.
	for _, conn := range dnsConns.clone() {
		// Check if connection is complete and attributed to the deleted profile.
		if conn.DataIsComplete() &&
			conn.ProcessContext.Profile == profileID &&
			conn.ProcessContext.Source == profileSource {

			reAttributeConnection(ctx, conn)
			reAttributed++
			tracer.Debugf("filter: re-attributed %s to %s", conn, conn.process.PrimaryProfileID)
		}
	}

	tracer.Infof("filter: re-attributed %d connections", reAttributed)
	return nil
}

func reAttributeConnection(ctx context.Context, conn *Connection) {
	// Check if data is complete.
	if !conn.DataIsComplete() {
		return
	}

	conn.Lock()
	defer conn.Unlock()

	// Attempt to assign new profile.
	err := conn.process.RefetchProfile(ctx)
	if err != nil {
		log.Warningf("network: failed to refetch profile for %s: %s", conn, err)
		return
	}

	// Set the new process context.
	conn.ProcessContext = getProcessContext(ctx, conn.process)
	conn.Save()

	// Trigger event for re-attribution.
	module.TriggerEvent(ConnectionReattributedEvent, conn.ID)
}
