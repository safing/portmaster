package service

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/safing/jess"
	"github.com/safing/portmaster/base/log"
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
				return fmt.Errorf("derive bin dir from runnning exe: %w", err)
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
		// FIXME: Select based on setting.
		sc.BinariesIndexURLs = DefaultStableBinaryIndexURLs
	}
	if len(sc.IntelIndexURLs) == 0 {
		sc.IntelIndexURLs = DefaultIntelIndexURLs
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
