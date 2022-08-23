package plugin

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/hashicorp/go-multierror"
	"github.com/safing/portbase/config"
	"github.com/safing/portbase/dataroot"
	"github.com/safing/portbase/log"
	"github.com/safing/portbase/modules"
	"github.com/safing/portbase/modules/subsystems"
	"github.com/safing/portmaster/network"
	"github.com/safing/portmaster/plugin/loader"
	"github.com/safing/portmaster/plugin/shared"
	"github.com/safing/portmaster/plugin/shared/proto"
)

var Module *ModuleImpl

type (
	ModuleImpl struct {
		*modules.Module

		// Loader is used to launch and dispense new plugins.
		// It also keeps track of running plugins.
		Loader *loader.PluginLoader

		// The configuration option on whether or not the plugin
		// system is enabled.
		PluginSystemEnabled config.BoolOption

		// deciders holds a list of loaded Decider plugins
		deciders []deciderPlugin

		// reporters holds a list of loaded Reporter plugins
		reporters []reporterPlugin
	}

	deciderPlugin struct {
		shared.Decider
		Name string
	}

	reporterPlugin struct {
		shared.Reporter
		Name string
	}
)

func init() {
	Module = &ModuleImpl{}
	m := modules.Register("plugin", Module.prepare, Module.start, Module.stop, "core")

	Module.Module = m

	subsystems.Register("plugins", "Plugins", "Portmaster Plugin Support", Module.Module, "config:plugins/", &config.Option{
		Name:            "Enable Plugin System",
		Key:             CfgKeyEnablePlugins,
		Description:     "Whether or not the internal Plugin System should be enabled",
		Help:            "TODO", // FIXME(ppacher)
		OptType:         config.OptTypeBool,
		DefaultValue:    false,
		ExpertiseLevel:  config.ExpertiseLevelDeveloper,
		ReleaseLevel:    config.ReleaseLevelExperimental,
		RequiresRestart: true,
		Annotations: config.Annotations{
			config.CategoryAnnotation: "General",
			config.DisplayHintOrdered: 255,
		},
	})
}

func (m *ModuleImpl) prepare() error {
	pluginDirectory := dataroot.Root().ChildDir("plugins", 0744)

	if err := pluginDirectory.Ensure(); err != nil {
		return fmt.Errorf("failed to prepare plugin directory: %w", err)
	}

	m.Loader = loader.NewPluginLoader(m.Module, pluginDirectory.Path)
	m.PluginSystemEnabled = config.Concurrent.GetAsBool(CfgKeyEnablePlugins, false)

	return nil
}

func (m *ModuleImpl) start() error {
	if !m.PluginSystemEnabled() {
		return nil
	}

	// try to parse the plugin configuration file
	configFile := filepath.Join(dataroot.Root().Path, "plugins.json")

	blob, err := os.ReadFile(configFile)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("failed to load configuration file: %w", err)
	}

	if err == nil {
		var pluginConfigs []PluginConfig
		if err := json.Unmarshal(blob, &pluginConfigs); err != nil {
			return fmt.Errorf("failed to parse plugin configuration: %w", err)
		}

		var multierr = new(multierror.Error)

		for _, pluginConfig := range pluginConfigs {
			for _, pluginType := range pluginConfig.Types {
				if pluginType == "base" {
					// "base" is implicit so we ignore it here
					continue
				}

				pluginImpl, err := m.Loader.Dispense(m.Ctx, pluginConfig.Name, pluginType)
				if err != nil {
					multierr.Errors = append(multierr.Errors, fmt.Errorf("failed to get plugin type %s from %s: %w", pluginType, pluginConfig.Name, err))

					continue
				}

				switch pluginType {
				case "decider":
					m.deciders = append(m.deciders, deciderPlugin{
						Decider: pluginImpl.(shared.Decider),
						Name:    pluginConfig.Name,
					})
				case "reporter":
					m.reporters = append(m.reporters, reporterPlugin{
						Reporter: pluginImpl.(shared.Reporter),
						Name:     pluginConfig.Name,
					})
				case "resolver":
					fallthrough
				default:
					multierr.Errors = append(multierr.Errors, fmt.Errorf("plugin type %s is not supported", pluginType))
				}
			}
		}

		if err := multierr.ErrorOrNil(); err != nil {
			log.Errorf("failed to dispense some plugins: %s", err)

			m.Module.Error("plugin-dispense-error", "Failed to dispense one or more plugins", err.Error())
		} else {
			m.Module.Resolve("plugin-dispense-error")
		}
	}

	return nil
}

// DecideOnConnection is called by the firewall to request a verdict decision from plugins.
// If the plugin system is disabled DecideOnConnection is a NO-OP.
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

func (m *ModuleImpl) stop() error {
	// Kill all running plugins.
	// TODO(ppacher): add support to re-attach to running plugins
	// by persisting the ReattachConfig of the *plugin.Client
	// See comment in loader.PluginLoader#Dispense
	m.Loader.Kill()

	return nil
}
