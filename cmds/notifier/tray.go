package main

import (
	"flag"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"fyne.io/systray"

	icons "github.com/safing/portmaster/assets"
	"github.com/safing/portmaster/base/log"
)

const (
	shortenStatusMsgTo = 40
)

var (
	trayLock sync.Mutex

	scaleColoredIconsTo int

	activeIconID    int = -1
	activeStatusMsg     = ""
	activeSPNStatus     = ""
	activeSPNSwitch     = ""

	menuItemStatusMsg *systray.MenuItem
	menuItemSPNStatus *systray.MenuItem
	menuItemSPNSwitch *systray.MenuItem
)

func init() {
	flag.IntVar(&scaleColoredIconsTo, "scale-icons", 32, "scale colored icons to given size in pixels")

	// lock until ready
	trayLock.Lock()
}

func tray() {
	if scaleColoredIconsTo > 0 {
		icons.ScaleColoredIconsTo(scaleColoredIconsTo)
	}

	systray.Run(onReady, onExit)
}

func exitTray() {
	systray.Quit()
}

func onReady() {
	// unlock when ready
	defer trayLock.Unlock()

	// icon
	systray.SetIcon(icons.ColoredIcons[icons.RedID])
	if runtime.GOOS == "windows" {
		// systray.SetTitle("Portmaster Notifier") // Don't set title, as it may be displayed in full in the menu/tray bar. (Ubuntu)
		systray.SetTooltip("Portmaster Notifier")
	}

	// menu: open app
	if dataDir != "" {
		menuItemOpenApp := systray.AddMenuItem("Open App", "")
		go clickListener(menuItemOpenApp, launchApp)
		systray.AddSeparator()
	}

	// menu: status

	menuItemStatusMsg = systray.AddMenuItem("Loading...", "")
	menuItemStatusMsg.Disable()
	systray.AddSeparator()

	// menu: SPN

	menuItemSPNStatus = systray.AddMenuItem("Loading...", "")
	menuItemSPNStatus.Disable()
	menuItemSPNSwitch = systray.AddMenuItem("Loading...", "")
	go clickListener(menuItemSPNSwitch, func() {
		ToggleSPN()
	})
	systray.AddSeparator()

	// menu: quit
	systray.AddSeparator()
	closeTray := systray.AddMenuItem("Close Tray Notifier", "")
	go clickListener(closeTray, func() {
		cancelMainCtx()
	})
	shutdownPortmaster := systray.AddMenuItem("Shut Down Portmaster", "")
	go clickListener(shutdownPortmaster, func() {
		_ = TriggerShutdown()
		time.Sleep(1 * time.Second)
		cancelMainCtx()
	})
}

func onExit() {
}

func triggerTrayUpdate() {
	// TODO: Deduplicate triggers.
	go updateTray()
}

// updateTray update the state of the tray depending on the currently available information.
func updateTray() {
	// Get current information.
	spnStatus := GetSPNStatus()
	failureID, failureMsg := GetFailure()

	trayLock.Lock()
	defer trayLock.Unlock()

	// Select icon and status message to show.
	newIconID := icons.GreenID
	newStatusMsg := "Secure"
	switch {
	case shuttingDown.IsSet():
		newIconID = icons.RedID
		newStatusMsg = "Shutting Down Portmaster"

	case restarting.IsSet():
		newIconID = icons.YellowID
		newStatusMsg = "Restarting Portmaster"

	case !connected.IsSet():
		newIconID = icons.RedID
		newStatusMsg = "Waiting for Portmaster Core Service"

	case failureID == FailureError:
		newIconID = icons.RedID
		newStatusMsg = failureMsg

	case failureID == FailureWarning:
		newIconID = icons.YellowID
		newStatusMsg = failureMsg

	case spnEnabled.IsSet():
		newIconID = icons.BlueID
	}

	// Set icon if changed.
	if newIconID != activeIconID {
		activeIconID = newIconID
		systray.SetIcon(icons.ColoredIcons[activeIconID])
	}

	// Set message if changed.
	if newStatusMsg != activeStatusMsg {
		activeStatusMsg = newStatusMsg

		// Shorten message if too long.
		shortenedMsg := activeStatusMsg
		if len(shortenedMsg) > shortenStatusMsgTo && strings.Contains(shortenedMsg, ". ") {
			shortenedMsg = strings.SplitN(shortenedMsg, ". ", 2)[0]
		}
		if len(shortenedMsg) > shortenStatusMsgTo {
			shortenedMsg = shortenedMsg[:shortenStatusMsgTo] + "..."
		}

		menuItemStatusMsg.SetTitle("Status: " + shortenedMsg)
	}

	// Set SPN status if changed.
	if spnStatus != nil && activeSPNStatus != spnStatus.Status {
		activeSPNStatus = spnStatus.Status
		menuItemSPNStatus.SetTitle("SPN: " + strings.Title(activeSPNStatus)) // nolint:staticcheck
	}

	// Set SPN switch if changed.
	newSPNSwitch := "Enable SPN"
	if spnEnabled.IsSet() {
		newSPNSwitch = "Disable SPN"
	}
	if activeSPNSwitch != newSPNSwitch {
		activeSPNSwitch = newSPNSwitch
		menuItemSPNSwitch.SetTitle(activeSPNSwitch)
	}
}

func clickListener(item *systray.MenuItem, fn func()) {
	for range item.ClickedCh {
		fn()
	}
}

func launchApp() {
	// build path to app
	pmStartPath := filepath.Join(dataDir, "portmaster-start")
	if runtime.GOOS == "windows" {
		pmStartPath += ".exe"
	}

	// start app
	cmd := exec.Command(pmStartPath, "app", "--data", dataDir)
	err := cmd.Start()
	if err != nil {
		log.Warningf("failed to start app: %s", err)
		return
	}

	// Use cmd.Wait() instead of cmd.Process.Release() to properly release its resources.
	// See https://github.com/golang/go/issues/36534
	go func() {
		err := cmd.Wait()
		if err != nil {
			log.Warningf("failed to wait/release app process: %s", err)
		}
	}()
}
