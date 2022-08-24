package plugin

import (
	"fmt"

	"github.com/hashicorp/go-multierror"
	"github.com/safing/portbase/log"
	"github.com/safing/portmaster/network"
	"github.com/safing/portmaster/plugin/shared/proto"
)

// DecideOnConnection is called by the firewall to request a verdict decision from plugins.
//
// If the plugin system is disabled DecideOnConnection is a no-op.
func (m *ModuleImpl) DecideOnConnection(conn *network.Connection) (network.Verdict, string, error) {
	if !m.PluginSystemEnabled() {
		return network.VerdictUndecided, "", nil
	}

	protoConn := proto.ConnectionFromNetwork(conn)

	var multierr = new(multierror.Error)
	defer func() {
		if err := multierr.ErrorOrNil(); err != nil {
			m.Module.Error("plugin-decider-error", "One or more decider plugins reported an error", err.Error())
		} else {
			m.Module.Resolve("plugin-decider-error")
		}
	}()

	for _, d := range m.deciders {
		log.Debugf("plugin: asking decider plugin %s for a verdict on %s", d.Name, conn.ID)
		verdict, reason, err := d.DecideOnConnection(m.Ctx, protoConn)
		if err != nil {
			// TODO(ppacher): capture the name of the plugin for this
			multierr.Errors = append(multierr.Errors, fmt.Errorf("plugin %s: %w", d.Name, err))

			continue
		}

		networkVerdict := proto.VerdictToNetwork(verdict)

		switch networkVerdict {
		case network.VerdictUndecided,
			network.VerdictUndeterminable,
			network.VerdictFailed:
			continue

		default:
			return networkVerdict, fmt.Sprintf("plugin %s: %s", d.Name, reason), nil
		}
	}

	return network.VerdictUndecided, "", nil
}

// ReportConnection is called by the firewall to report a connection verdict to any
// registered reporter plugin.
//
// If the plugin system is disabled ReportConnection is a no-op.
func (m *ModuleImpl) ReportConnection(conn *network.Connection) {
	if !m.PluginSystemEnabled() {
		return
	}

	protoConn := proto.ConnectionFromNetwork(conn)

	var multierr = new(multierror.Error)
	for _, r := range m.reporters {
		log.Debugf("plugin: reporting connection %s to %s", conn.ID, r.Name)

		if err := r.ReportConnection(m.Ctx, protoConn); err != nil {
			multierr.Errors = append(multierr.Errors, fmt.Errorf("plugin %s: %w", r.Name, err))
		}
	}

	if err := multierr.ErrorOrNil(); err != nil {
		log.Errorf("plugin: one or more reporter plugins returned an error: %s", err)

		m.Module.Error("plugin-reporter-error", "One or more reporter plugins reported an error", err.Error())
	} else {
		m.Module.Resolve("plugin-reporter-error")
	}
}
