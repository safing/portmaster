package plugin

import (
	"context"
	"fmt"
	"time"

	"github.com/hashicorp/go-multierror"
	"github.com/miekg/dns"
	"github.com/safing/portbase/log"
	"github.com/safing/portmaster/network"
	"github.com/safing/portmaster/plugin/internal"
	"github.com/safing/portmaster/resolver"
)

func (m *ModuleImpl) Resolve(msg *dns.Msg, conn *network.Connection) (*dns.Msg, *resolver.ResolverInfo, error) {
	if !m.PluginSystemEnabled() {
		return nil, nil, nil
	}

	if len(msg.Question) == 0 {
		return nil, nil, nil
	}

	protoConn := internal.ConnectionFromNetwork(conn)
	protoQuestion := internal.DNSQuestionToProto(msg.Question[0])

	var multierr = new(multierror.Error)
	defer func() {
		if err := multierr.ErrorOrNil(); err != nil {
			m.Module.Error("plugin-resolver-error", "One or more resolver plugins reported an error", err.Error())
		} else {
			m.Module.Resolve("plugin-resolver-error")
		}
	}()

	m.l.RLock()
	defer m.l.RUnlock()

L:
	for _, inst := range m.plugins {
		// do not give plugins more than 2 seconds for deciding on a connection.
		ctx, cancel := context.WithTimeout(m.Ctx, 2*time.Second)

		log.Debugf("plugin: trying to resolve %s using plugin %s", protoQuestion.GetName(), inst.Name)
		res, err := inst.Resolve(ctx, protoQuestion, protoConn)
		cancel()

		if err != nil {
			multierr.Errors = append(multierr.Errors, fmt.Errorf("plugin %s: %w", inst.Name, err))

			continue
		}

		if res != nil {
			answers := make([]dns.RR, len(res.GetRrs()))
			for idx, rr := range res.GetRrs() {
				var err error
				answers[idx], err = internal.DNSRRFromProto(rr)
				if err != nil {
					multierr.Errors = append(multierr.Errors, fmt.Errorf("plugin %s: %w", inst.Name, err))

					continue L
				}
			}

			reply := new(dns.Msg)

			reply.Answer = answers
			reply.SetRcode(msg, int(res.GetRcode()))

			return reply, &resolver.ResolverInfo{
				Name: inst.Name,
				Type: "plugin",
			}, nil
		}
	}

	return nil, nil, nil
}

// DecideOnConnection is called by the firewall to request a verdict decision from plugins.
//
// If the plugin system is disabled DecideOnConnection is a no-op.
func (m *ModuleImpl) DecideOnConnection(conn *network.Connection) (network.Verdict, string, error) {
	if !m.PluginSystemEnabled() {
		return network.VerdictUndecided, "", nil
	}

	protoConn := internal.ConnectionFromNetwork(conn)

	var multierr = new(multierror.Error)
	defer func() {
		if err := multierr.ErrorOrNil(); err != nil {
			m.Module.Error("plugin-decider-error", "One or more decider plugins reported an error", err.Error())
		} else {
			m.Module.Resolve("plugin-decider-error")
		}
	}()

	m.l.RLock()
	defer m.l.RUnlock()

	for _, d := range m.plugins {
		// do not give plugins more than 2 seconds for deciding on a connection.
		ctx, cancel := context.WithTimeout(m.Ctx, 2*time.Second)

		log.Debugf("plugin: asking decider plugin %s for a verdict on %s", d.Name, conn.ID)
		verdict, reason, err := d.DecideOnConnection(ctx, protoConn)

		cancel()

		if err != nil {
			multierr.Errors = append(multierr.Errors, fmt.Errorf("plugin %s: %w", d.Name, err))

			continue
		}

		networkVerdict := internal.VerdictToNetwork(verdict)

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

	protoConn := internal.ConnectionFromNetwork(conn)

	var multierr = new(multierror.Error)

	defer func() {
		if err := multierr.ErrorOrNil(); err != nil {
			log.Errorf("plugin: one or more reporter plugins returned an error: %s", err)

			m.Module.Error("plugin-reporter-error", "One or more reporter plugins reported an error", err.Error())
		} else {
			m.Module.Resolve("plugin-reporter-error")
		}
	}()

	m.l.RLock()
	defer m.l.RUnlock()

	for _, r := range m.plugins {
		log.Debugf("plugin: reporting connection %s to %s", conn.ID, r.Name)

		if err := r.ReportConnection(m.Ctx, protoConn); err != nil {
			multierr.Errors = append(multierr.Errors, fmt.Errorf("plugin %s: %w", r.Name, err))
		}
	}
}
