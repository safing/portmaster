package config

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"github.com/safing/portmaster/base/dataroot"
	"github.com/safing/portmaster/base/utils"
	"github.com/safing/portmaster/base/utils/debug"
	"github.com/safing/portmaster/service/mgr"
)

// ChangeEvent is the name of the config change event.
const ChangeEvent = "config change"

var (
	dataRoot *utils.DirStructure

	exportConfig bool
)

// SetDataRoot sets the data root from which the updates module derives its paths.
func SetDataRoot(root *utils.DirStructure) {
	if dataRoot == nil {
		dataRoot = root
	}
}

func init() {
	flag.BoolVar(&exportConfig, "export-config-options", false, "export configuration registry and exit")
}

func prep() error {
	SetDataRoot(dataroot.Root())
	if dataRoot == nil {
		return errors.New("data root is not set")
	}

	if exportConfig {
		module.instance.SetCmdLineOperation(exportConfigCmd)
		return mgr.ErrExecuteCmdLineOp
	}

	return registerBasicOptions()
}

func start() error {
	configFilePath = filepath.Join(dataRoot.Path, "config.json")

	// Load log level from log package after it started.
	err := loadLogLevel()
	if err != nil {
		return err
	}

	err = registerAsDatabase()
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}

	err = loadConfig(false)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("failed to load config file: %w", err)
	}
	return nil
}

func exportConfigCmd() error {
	// Reset the metrics instance name option, as the default
	// is set to the current hostname.
	// Config key copied from metrics.CfgOptionInstanceKey.
	option, err := GetOption("core/metrics/instance")
	if err == nil {
		option.DefaultValue = ""
	}

	data, err := json.MarshalIndent(ExportOptions(), "", "  ")
	if err != nil {
		return err
	}

	_, err = os.Stdout.Write(data)
	return err
}

// AddToDebugInfo adds all changed global config options to the given debug.Info.
func AddToDebugInfo(di *debug.Info) {
	var lines []string

	// Collect all changed settings.
	_ = ForEachOption(func(opt *Option) error {
		opt.Lock()
		defer opt.Unlock()

		if opt.ReleaseLevel <= getReleaseLevel() && opt.activeValue != nil {
			if opt.Sensitive {
				lines = append(lines, fmt.Sprintf("%s: [redacted]", opt.Key))
			} else {
				lines = append(lines, fmt.Sprintf("%s: %v", opt.Key, opt.activeValue.getData(opt)))
			}
		}

		return nil
	})
	sort.Strings(lines)

	// Add data as section.
	di.AddSection(
		fmt.Sprintf("Config: %d", len(lines)),
		debug.UseCodeSection|debug.AddContentLineBreaks,
		lines...,
	)
}

// GetActiveConfigValues returns a map with the active config values.
func GetActiveConfigValues() map[string]interface{} {
	values := make(map[string]interface{})

	// Collect active values from options.
	_ = ForEachOption(func(opt *Option) error {
		opt.Lock()
		defer opt.Unlock()

		if opt.ReleaseLevel <= getReleaseLevel() && opt.activeValue != nil {
			values[opt.Key] = opt.activeValue.getData(opt)
		}

		return nil
	})

	return values
}

// InitializeUnitTestDataroot initializes a new random tmp directory for running tests.
func InitializeUnitTestDataroot(testName string) (string, error) {
	basePath, err := os.MkdirTemp("", fmt.Sprintf("portmaster-%s", testName))
	if err != nil {
		return "", fmt.Errorf("failed to make tmp dir: %w", err)
	}

	ds := utils.NewDirStructure(basePath, utils.PublicReadPermission)
	SetDataRoot(ds)
	err = dataroot.Initialize(basePath, utils.PublicReadPermission)
	if err != nil {
		return "", fmt.Errorf("failed to initialize dataroot: %w", err)
	}

	return basePath, nil
}
