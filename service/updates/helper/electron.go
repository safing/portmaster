package helper

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/base/updater"
)

var pmElectronUpdate *updater.File

const suidBitWarning = `Failed to set SUID permissions for chrome-sandbox. This is required for Linux kernel versions that do not have unprivileged user namespaces (CONFIG_USER_NS_UNPRIVILEGED) enabled. If you're running and up-to-date distribution kernel you can likely ignore this warning. If you encounter issue starting the user interface please either update your kernel or set the SUID bit (mode 0%0o) on %s`

// EnsureChromeSandboxPermissions makes sure the chrome-sandbox distributed
// by our app-electron package has the SUID bit set on systems that do not
// allow unprivileged CLONE_NEWUSER (clone(3)).
// On non-linux systems or systems that have kernel.unprivileged_userns_clone
// set to 1 EnsureChromeSandboPermissions is a NO-OP.
func EnsureChromeSandboxPermissions(reg *updater.ResourceRegistry) error {
	if runtime.GOOS != "linux" {
		return nil
	}

	if pmElectronUpdate != nil && !pmElectronUpdate.UpgradeAvailable() {
		return nil
	}

	identifier := PlatformIdentifier("app/portmaster-app.zip")

	var err error
	pmElectronUpdate, err = reg.GetFile(identifier)
	if err != nil {
		if errors.Is(err, updater.ErrNotAvailableLocally) {
			return nil
		}
		return fmt.Errorf("failed to get file: %w", err)
	}

	unpackedPath := strings.TrimSuffix(
		pmElectronUpdate.Path(),
		filepath.Ext(pmElectronUpdate.Path()),
	)
	sandboxFile := filepath.Join(unpackedPath, "chrome-sandbox")
	if err := os.Chmod(sandboxFile, 0o0755|os.ModeSetuid); err != nil {
		log.Errorf(suidBitWarning, 0o0755|os.ModeSetuid, sandboxFile)
		return fmt.Errorf("failed to chmod: %w", err)
	}

	log.Debugf("updates: fixed SUID permission for chrome-sandbox")

	return nil
}
