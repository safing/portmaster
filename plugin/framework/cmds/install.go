package cmd

import (
	"encoding/json"
	"io"
	"log"
	"os"
	"path/filepath"

	"github.com/safing/portmaster/plugin/shared"
	"github.com/spf13/cobra"
)

type InstallCommandConfig struct {
	PluginName   string
	StaticConfig json.RawMessage
	Types        []shared.PluginType
}

func InstallCommand(cfg *InstallCommandConfig) *cobra.Command {
	var installDir string

	cmd := &cobra.Command{
		Use: "install",
		Run: func(cmd *cobra.Command, args []string) {
			// create the plugin directory
			pluginDir := filepath.Join(installDir, "plugins")
			if err := os.Mkdir(pluginDir, 0755); err != nil && !os.IsExist(err) {
				log.Fatalf("failed to create plugin directory: %s", err)
			}

			// get the path to the executable
			execPath, err := os.Executable()
			if err != nil {
				log.Fatalf("failed to determine executable path: %s", err)
			}

			// get the name of the plugin
			pluginName := filepath.Base(execPath)
			if cfg != nil && cfg.PluginName != "" {
				pluginName = cfg.PluginName
			}

			pluginTarget := filepath.Join(pluginDir, pluginName)

			// delete the plugin if it exists already
			if err := os.Remove(pluginTarget); err != nil && !os.IsNotExist(err) {
				log.Fatalf("failed to remove previous plugin version: %s", err)
			}

			// open our self
			source, err := os.Open(execPath)
			if err != nil {
				log.Fatalf("failed to open plugin executable: %s", err)
			}
			defer source.Close()

			// create the executable into the plugins directory
			target, err := os.OpenFile(pluginTarget, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0555)
			if err != nil {
				log.Fatalf("failed to create target file for plugin: %s", err)
			}

			// copy the binary to the new location
			if _, err := io.Copy(target, source); err != nil {
				target.Close()
				os.Remove(target.Name())

				log.Fatalf("failed to copy binary: %s", err)
			}

			target.Close()

			// try to open and read the plugins.json file
			pluginJson := filepath.Join(installDir, "plugins.json")
			var cfgs []shared.PluginConfig

			blob, err := os.ReadFile(pluginJson)
			if err != nil && !os.IsNotExist(err) {
				log.Fatalf("failed to read plugins.json: %s", err)
			}

			if err == nil {
				if err := json.Unmarshal(blob, &cfgs); err != nil {
					log.Fatalf("failed to parse plugins.json: %s", err)
				}
			}

			for idx, cfg := range cfgs {
				if cfg.Name == pluginName {
					cfgs = append(cfgs[:idx], cfgs[idx+1:]...)

					break
				}
			}

			var types []shared.PluginType
			var config json.RawMessage
			if cfg != nil {
				types = cfg.Types
				config = cfg.StaticConfig
			}

			// add the test plugin at the first position
			cfgs = append([]shared.PluginConfig{
				{
					Name:   pluginName,
					Types:  types,
					Config: config,
				},
			}, cfgs...)

			// marshal the configuration and write the pluginJson
			blob, err = json.MarshalIndent(cfgs, "", "    ")
			if err != nil {
				log.Fatalf("failed to marshal JSON configuration file")
			}

			if err := os.WriteFile(pluginJson, blob, 0644); err != nil {
				log.Fatalf("failed to write plugins.json: %s", err)
			}

			log.Printf("%s successfully installed. Please enable the Plugin System in the UI and restart the Portmaster", pluginName)
		},
	}

	flags := cmd.Flags()
	{
		flags.StringVarP(&installDir, "data", "d", "/opt/safing/portmaster", "Path to the Portmaster installation directory")
	}

	return cmd
}
