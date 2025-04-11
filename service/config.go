package service

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/safing/jess"
	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/service/configure"
	"github.com/safing/portmaster/service/updates"
)

type ServiceConfig struct {
	BinDir  string
	DataDir string

	LogToStdout bool
	LogDir      string
	LogLevel    string

	BinariesIndexURLs   []string
	IntelIndexURLs      []string
	VerifyBinaryUpdates jess.TrustStore
	VerifyIntelUpdates  jess.TrustStore
}

func (sc *ServiceConfig) Init() error {
	// Check directories.
	switch runtime.GOOS {
	case "windows":
		// Fall back to defaults.
		if sc.BinDir == "" {
			exeDir, err := getCurrentBinaryFolder() // Default: C:/Program Files/Portmaster
			if err != nil {
				return fmt.Errorf("derive bin dir from running exe: %w", err)
			}
			sc.BinDir = exeDir
		}
		if sc.DataDir == "" {
			sc.DataDir = filepath.FromSlash("$ProgramData/Portmaster")
		}
		if sc.LogDir == "" {
			sc.LogDir = filepath.Join(sc.DataDir, "logs")
		}

	case "linux":
		// Fall back to defaults.
		if sc.BinDir == "" {
			sc.BinDir = "/usr/lib/portmaster"
		}
		if sc.DataDir == "" {
			sc.DataDir = "/var/lib/portmaster"
		}
		if sc.LogDir == "" {
			sc.LogDir = "/var/log/portmaster"
		}

	default:
		// Fail if not configured on other platforms.
		if sc.BinDir == "" {
			return errors.New("binary directory must be configured - auto-detection not supported on this platform")
		}
		if sc.DataDir == "" {
			return errors.New("binary directory must be configured - auto-detection not supported on this platform")
		}
		if !sc.LogToStdout && sc.LogDir == "" {
			return errors.New("logging directory must be configured - auto-detection not supported on this platform")
		}
	}

	// Expand path variables.
	sc.BinDir = os.ExpandEnv(sc.BinDir)
	sc.DataDir = os.ExpandEnv(sc.DataDir)
	sc.LogDir = os.ExpandEnv(sc.LogDir)

	// Apply defaults for required fields.
	if len(sc.BinariesIndexURLs) == 0 {
		sc.BinariesIndexURLs = configure.DefaultStableBinaryIndexURLs
	}
	if len(sc.IntelIndexURLs) == 0 {
		sc.IntelIndexURLs = configure.DefaultIntelIndexURLs
	}

	// Check log level.
	if sc.LogLevel != "" && log.ParseLevel(sc.LogLevel) == 0 {
		return fmt.Errorf("invalid log level %q", sc.LogLevel)
	}

	return nil
}

func getCurrentBinaryFolder() (string, error) {
	// Get the path of the currently running executable
	exePath, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("failed to get executable path: %w", err)
	}

	// Get the absolute path
	absPath, err := filepath.Abs(exePath)
	if err != nil {
		return "", fmt.Errorf("failed to get absolute path: %w", err)
	}

	// Get the directory of the executable
	installDir := filepath.Dir(absPath)

	return installDir, nil
}

func MakeUpdateConfigs(svcCfg *ServiceConfig) (binaryUpdateConfig, intelUpdateConfig *updates.Config, err error) {
	switch runtime.GOOS {
	case "windows":
		binaryUpdateConfig = &updates.Config{
			Name:              configure.DefaultBinaryIndexName,
			Directory:         svcCfg.BinDir,
			DownloadDirectory: filepath.Join(svcCfg.DataDir, "download_binaries"),
			PurgeDirectory:    filepath.Join(svcCfg.BinDir, "upgrade_obsolete_binaries"),
			Ignore:            []string{"databases", "intel", "config.json"},
			IndexURLs:         svcCfg.BinariesIndexURLs, // May be changed by config during instance startup.
			IndexFile:         "index.json",
			Verify:            svcCfg.VerifyBinaryUpdates,
			AutoCheck:         true, // May be changed by config during instance startup.
			AutoDownload:      false,
			AutoApply:         false,
			NeedsRestart:      true,
			Notify:            true,
		}
		intelUpdateConfig = &updates.Config{
			Name:              configure.DefaultIntelIndexName,
			Directory:         filepath.Join(svcCfg.DataDir, "intel"),
			DownloadDirectory: filepath.Join(svcCfg.DataDir, "download_intel"),
			PurgeDirectory:    filepath.Join(svcCfg.DataDir, "upgrade_obsolete_intel"),
			IndexURLs:         svcCfg.IntelIndexURLs,
			IndexFile:         "index.json",
			Verify:            svcCfg.VerifyIntelUpdates,
			AutoCheck:         true, // May be changed by config during instance startup.
			AutoDownload:      true,
			AutoApply:         true,
			NeedsRestart:      false,
			Notify:            false,
		}

	case "linux":
		binaryUpdateConfig = &updates.Config{
			Name:              configure.DefaultBinaryIndexName,
			Directory:         svcCfg.BinDir,
			DownloadDirectory: filepath.Join(svcCfg.DataDir, "download_binaries"),
			PurgeDirectory:    filepath.Join(svcCfg.DataDir, "upgrade_obsolete_binaries"),
			Ignore:            []string{"databases", "intel", "config.json"},
			IndexURLs:         svcCfg.BinariesIndexURLs, // May be changed by config during instance startup.
			IndexFile:         "index.json",
			Verify:            svcCfg.VerifyBinaryUpdates,
			AutoCheck:         true, // May be changed by config during instance startup.
			AutoDownload:      false,
			AutoApply:         false,
			NeedsRestart:      true,
			Notify:            true,
		}
		intelUpdateConfig = &updates.Config{
			Name:              configure.DefaultIntelIndexName,
			Directory:         filepath.Join(svcCfg.DataDir, "intel"),
			DownloadDirectory: filepath.Join(svcCfg.DataDir, "download_intel"),
			PurgeDirectory:    filepath.Join(svcCfg.DataDir, "upgrade_obsolete_intel"),
			IndexURLs:         svcCfg.IntelIndexURLs,
			IndexFile:         "index.json",
			Verify:            svcCfg.VerifyIntelUpdates,
			AutoCheck:         true, // May be changed by config during instance startup.
			AutoDownload:      true,
			AutoApply:         true,
			NeedsRestart:      false,
			Notify:            false,
		}
	}

	return
}
