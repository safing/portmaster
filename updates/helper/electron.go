package helper

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/safing/portbase/log"
	"github.com/safing/portbase/updater"
)

var pmElectronUpdate *updater.File

// EnsureChromeSandboxPermissions makes sure the chrome-sandbox distributed
// by our app-electron package has the SUID bit set on systems that do not
// allow unprivileged CLONE_NEWUSER (clone(3)).
// On non-linux systems or systems that have kernel.unprivileged_userns_clone
// set to 1 EnsureChromeSandboPermissions is a NO-OP.
func EnsureChromeSandboxPermissions(reg *updater.ResourceRegistry) error {
	if runtime.GOOS != "linux" {
		return nil
	}

	_, err := os.Stat("/proc/self/ns/user")
	if err == nil {
		return nil
	}
	if !os.IsNotExist(err) {
		return err
	}
	// err == ENOENT

	if pmElectronUpdate != nil && !pmElectronUpdate.UpgradeAvailable() {
		return nil
	}
	identifier := PlatformIdentifier("app/portmaster-app.zip")

	log.Debug("updates: kernel support for unprivileged USERNS_CLONE disabled")

	pmElectronUpdate, err = reg.GetFile(identifier)
	if err != nil {
		return err
	}
	unpackedPath := strings.TrimSuffix(
		pmElectronUpdate.Path(),
		filepath.Ext(pmElectronUpdate.Path()),
	)
	sandboxFile := filepath.Join(unpackedPath, "chrome-sandbox")
	if err := os.Chmod(sandboxFile, 0755|os.ModeSetuid); err != nil {
		return err
	}
	log.Infof("updates: fixed SUID permission for chrome-sandbox")

	return nil
}

func checkSysctl(setting string, value byte) bool {
	c, err := sysctl(setting)
	if err != nil {
		return false
	}
	if len(c) < 1 {
		return false
	}
	return c[0] == value
}

func sysctl(setting string) ([]byte, error) {
	parts := append([]string{"/proc", "sys"}, strings.Split(setting, ".")...)
	path := filepath.Join(parts...)
	content, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return content, nil
}
