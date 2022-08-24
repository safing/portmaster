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
	"github.com/safing/portmaster/plugin/loader"
	"github.com/safing/portmaster/plugin/shared"
	"github.com/safing/portmaster/plugin/shared/decider"
	"github.com/safing/portmaster/plugin/shared/reporter"
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
		decider.Decider
		Name string
	}

	reporterPlugin struct {
		reporter.Reporter
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

		if err := m.loadPlugins(pluginConfigs); err != nil {
			log.Errorf("failed to dispense some plugins: %s", err)

			m.Module.Error("plugin-dispense-error", "Failed to dispense one or more plugins", err.Error())
		} else {
			m.Module.Resolve("plugin-dispense-error")
		}
	}

	return nil
}

func (m *ModuleImpl) loadPlugins(cfgs []PluginConfig) error {
	var multierr = new(multierror.Error)

	for _, pluginConfig := range cfgs {
		for _, pluginType := range pluginConfig.Types {
			// make sure we have a valid plugin type
			if !shared.IsValidPluginType(pluginType) {
				multierr.Errors = append(multierr.Errors, fmt.Errorf("unsupported plugin type %s", pluginType))

				continue
			}

			// "base" is implicit so we ignore it here
			if pluginType == "base" {
				continue
			}

			pluginImpl, err := m.Loader.Dispense(m.Ctx, pluginConfig.Name, pluginType, pluginConfig.Config)
			if err != nil {
				multierr.Errors = append(multierr.Errors, fmt.Errorf("failed to get plugin type %s from %s: %w", pluginType, pluginConfig.Name, err))

				continue
			}

			switch pluginType {
			case "decider":
				m.deciders = append(m.deciders, deciderPlugin{
					Decider: pluginImpl.(decider.Decider),
					Name:    pluginConfig.Name,
				})
			case "reporter":
				m.reporters = append(m.reporters, reporterPlugin{
					Reporter: pluginImpl.(reporter.Reporter),
					Name:     pluginConfig.Name,
				})
			case "resolver":
				fallthrough
			default:
				multierr.Errors = append(multierr.Errors, fmt.Errorf("plugin type %s is not supported", pluginType))
			}
		}
	}

	return multierr.ErrorOrNil()
}

func (m *ModuleImpl) stop() error {
	// Kill all running plugins.
	// TODO(ppacher): add support to re-attach to running plugins
	// by persisting the ReattachConfig of the *plugin.Client
	// See comment in loader.PluginLoader#Dispense
	m.Loader.Kill()

	return nil
}
