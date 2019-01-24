package ui

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"runtime"

	"github.com/Safing/portbase/modules"
	"github.com/Safing/portmaster/updates"
)

var (
	launchUI bool
)

func init() {
	flag.BoolVar(&launchUI, "ui", false, "launch user interface and exit")
}

func launchUIByFlag() error {
	if !launchUI {
		return nil
	}

	err := updates.ReloadLatest()
	if err != nil {
		return err
	}

	osAndPlatform := fmt.Sprintf("%s_%s", runtime.GOOS, runtime.GOARCH)

	switch osAndPlatform {
	case "linux_amd64":

		file, err := updates.GetPlatformFile("app/portmaster-ui")
		if err != nil {
			return fmt.Errorf("ui currently not available: %s - you may need to first start portmaster and wait for it to fetch the update index", err)
		}

		// check permission
		info, err := os.Stat(file.Path())
		if info.Mode() != 0755 {
			fmt.Printf("%v\n", info.Mode())
			err := os.Chmod(file.Path(), 0755)
			if err != nil {
				return fmt.Errorf("failed to set exec permissions on %s: %s", file.Path(), err)
			}
		}

		// exec
		cmd := exec.Command(file.Path())
		err = cmd.Start()
		if err != nil {
			return fmt.Errorf("failed to start ui: %s", err)
		}

		// gracefully exit portmaster
		return modules.ErrCleanExit

	default:
		return errors.New("this os/platform is no UI support yet")
	}
}
