package main

import (
	"fmt"
	"os"
	"strings"

	"golang.org/x/sys/windows/registry"
	"golang.org/x/sys/windows/svc"

	"github.com/safing/portmaster/cmds/cmdbase"
	"github.com/spf13/cobra"
)

func runPlatformSpecifics(cmd *cobra.Command, args []string) {
	switch {
	case printVersion:
		runFlagCmd(cmdbase.Version, cmd, args)
	}

	// Ensure the service ImagePath in the registry is properly quoted (CWE-428).
	// Self-heals installations created by older installers without requiring a
	// manual re-install.
	if isService, err := svc.IsWindowsService(); err == nil && isService {
		if err := ensureQuotedServiceImagePath(); err != nil {
			fmt.Fprintf(os.Stderr, "cwe428 check error: %v\n", err)
		}
	}
}

// ensureQuotedServiceImagePath detects and corrects an unquoted ImagePath in the
// PortmasterCore service registry entry (CWE-428 / unquoted service path LPE).
//
// Older installers (before v2.1.19) registered the service with an unquoted
// binary path, e.g.:
//
//	C:\Program Files\Portmaster\portmaster-core.exe --log-dir=...
//
// Windows SCM resolves such paths by testing each space-delimited prefix as an
// executable, so C:\Program.exe would be tried first — allowing a local attacker
// to achieve code execution as SYSTEM.
//
// This function self-heals affected installations on the first run of the patched
// binary, so users who update via the in-app updater (without re-running the
// installer) are also protected. It is idempotent: once the path is quoted the
// HasPrefix check exits immediately on every subsequent start.
//
// The service runs as SYSTEM and therefore has the required registry write access.
func ensureQuotedServiceImagePath() error {
	const regPath = `SYSTEM\CurrentControlSet\Services\PortmasterCore`

	key, err := registry.OpenKey(registry.LOCAL_MACHINE, regPath, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("failed to open service registry key: %w", err)
	}
	defer key.Close()

	imagePath, _, err := key.GetStringValue("ImagePath")
	if err != nil {
		return fmt.Errorf("failed to read ImagePath: %w", err)
	}

	// Already correctly quoted — nothing to do.
	if strings.HasPrefix(imagePath, `"`) {
		return nil
	}

	// Unquoted path detected. Locate the end of the executable (.exe boundary).
	exeEnd := strings.Index(strings.ToLower(imagePath), ".exe")
	if exeEnd < 0 {
		return fmt.Errorf("ImagePath contains no .exe, skipping fix: %s", imagePath)
	}
	exeEnd += len(".exe")

	exePath := imagePath[:exeEnd]
	rest := strings.TrimSpace(imagePath[exeEnd:])

	// Only fix the entry if it points to our own binary.
	// This prevents accidental rewrites if the install location changes
	// or if the registry entry has been reconfigured to point elsewhere.
	selfPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to resolve current executable path, skipping fix: %w", err)
	}
	if !strings.EqualFold(exePath, selfPath) {
		return fmt.Errorf("ImagePath executable does not match current binary, skipping fix: imagePath=%s self=%s", exePath, selfPath)
	}

	var fixed string
	if rest != "" {
		fixed = `"` + exePath + `" ` + rest
	} else {
		fixed = `"` + exePath + `"`
	}

	if err := key.SetStringValue("ImagePath", fixed); err != nil {
		return fmt.Errorf("failed to write fixed ImagePath: %w", err)
	}

	fmt.Fprintf(os.Stdout, "cwe428 check: fixed unquoted service ImagePath: old=%s new=%s\n", imagePath, fixed)
	return nil
}
