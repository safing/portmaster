package ui

import (
	"flag"
	"fmt"
	"os"
	"os/exec"

	"github.com/Safing/portbase/modules"
	"github.com/Safing/portmaster/updates"
)

var (
	launchUI       bool
	launchNotifier bool
)

func init() {
	flag.BoolVar(&launchUI, "ui", false, "launch user interface and exit")
	flag.BoolVar(&launchNotifier, "notifier", false, "launch notifier and exit")
}

func launchUIByFlag() error {
	if !launchUI && !launchNotifier {
		return nil
	}

	err := updates.ReloadLatest()
	if err != nil {
		return err
	}

	if launchUI {
		err = launch("app/portmaster-ui")
		if err != nil {
			return err
		}
	}

	if launchNotifier {
		return launch("notifier/portmaster-notifier")
	}

	return nil
}

func launch(identifier string) error {
	file, err := updates.GetPlatformFile(identifier)
	if err != nil {
		return fmt.Errorf("%s currently not available: %s - you may need to first start portmaster and wait for it to fetch the update index", identifier, err)
	}

	// check permission
	info, err := os.Stat(file.Path())
	if info.Mode() != 0755 {
		err := os.Chmod(file.Path(), 0755)
		if err != nil {
			return fmt.Errorf("failed to set exec permissions on %s: %s", file.Path(), err)
		}
	}

	// exec
	cmd := exec.Command(file.Path())
	err = cmd.Start()
	if err != nil {
		return fmt.Errorf("failed to start %s: %s", identifier, err)
	}

	// gracefully exit portmaster
	return modules.ErrCleanExit
}
