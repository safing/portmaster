package main

import (
	"fmt"
	"sync"

	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/cmds/notifier/wintoast"
	"github.com/safing/portmaster/service/updates/helper"
)

type NotificationID int64

const (
	appName              = "Portmaster"
	appUserModelID       = "io.safing.portmaster.2"
	originalShortcutPath = "C:\\ProgramData\\Microsoft\\Windows\\Start Menu\\Programs\\Portmaster\\Portmaster.lnk"
)

const (
	SoundDefault = 0
	SoundSilent  = 1
	SoundLoop    = 2
)

const (
	SoundPathDefault = 0
	// see notification_glue.h if you need more types
)

var (
	initOnce           sync.Once
	lib                *wintoast.WinToast
	notificationsByIDs sync.Map
)

func getLib() *wintoast.WinToast {
	initOnce.Do(func() {
		dllPath, err := getDllPath()
		if err != nil {
			log.Errorf("notify: failed to get dll path: %s", err)
			return
		}
		// Load dll and all the functions
		newLib, err := wintoast.New(dllPath)
		if err != nil {
			log.Errorf("notify: failed to load library: %s", err)
			return
		}

		// Initialize. This will create or update application shortcut. C:\Users\<user>\AppData\Roaming\Microsoft\Windows\Start Menu\Programs
		// and it will be of the originalShortcutPath with no CLSID and different AUMI
		err = newLib.Initialize(appName, appUserModelID, originalShortcutPath)
		if err != nil {
			log.Errorf("notify: failed to load library: %s", err)
			return
		}

		// library was initialized successfully
		lib = newLib

		// Set callbacks

		err = lib.SetCallbacks(notificationActivatedCallback, notificationDismissedCallback, notificationDismissedCallback)
		if err != nil {
			log.Warningf("notify: failed to set callbacks: %s", err)
			return
		}
	})

	return lib
}

// Show shows the notification.
func (n *Notification) Show() {
	// Lock notification
	n.Lock()
	defer n.Unlock()

	// Create new notification object
	builder, err := getLib().NewNotification(n.Title, n.Message)
	if err != nil {
		log.Errorf("notify: failed to create notification: %s", err)
		return
	}
	// Make sure memory is freed when done
	defer builder.Delete()

	// if needed set notification icon
	// _ = builder.SetImage(iconLocation)

	// Leaving the default value for the sound
	// _ = builder.SetSound(SoundDefault, SoundPathDefault)

	// Set all the required actions.
	for _, action := range n.AvailableActions {
		err = builder.AddButton(action.Text)
		if err != nil {
			log.Warningf("notify: failed to add button: %s", err)
		}
	}

	// Show notification.
	id, err := builder.Show()
	if err != nil {
		log.Errorf("notify: failed to show notification: %s", err)
		return
	}
	n.systemID = NotificationID(id)

	// Link system id to the notification object
	notificationsByIDs.Store(NotificationID(id), n)

	log.Debugf("notify: showing notification %q: %d", n.Title, n.systemID)
}

// Cancel cancels the notification.
func (n *Notification) Cancel() {
	// Lock notification
	n.Lock()
	defer n.Unlock()

	// No need to check for errors. If it fails it is probably already dismissed
	_ = getLib().HideNotification(int64(n.systemID))

	notificationsByIDs.Delete(n.systemID)
	log.Debugf("notify: notification canceled %q: %d", n.Title, n.systemID)
}

func notificationActivatedCallback(id int64, actionIndex int32) {
	if actionIndex == -1 {
		// The user clicked on the notification (not a button), open the portmaster and delete
		launchApp()
		notificationsByIDs.Delete(NotificationID(id))
		log.Debugf("notify: notification clicked %d", id)
		return
	}

	// The user click one of the buttons

	// Get notified object
	n, ok := notificationsByIDs.LoadAndDelete(NotificationID(id))
	if !ok {
		return
	}

	notification := n.(*Notification)

	notification.Lock()
	defer notification.Unlock()

	// Set selected action
	actionID := notification.AvailableActions[actionIndex].ID
	notification.SelectAction(actionID)

	log.Debugf("notify: notification button cliecked %d button id: %d", id, actionIndex)
}

func notificationDismissedCallback(id int64, reason int32) {
	// Failure or user dismissed the notification
	if reason == 0 {
		notificationsByIDs.Delete(NotificationID(id))
		log.Debugf("notify: notification dissmissed %d", id)
	}
}

func getDllPath() (string, error) {
	if dataDir == "" {
		return "", fmt.Errorf("dataDir is empty")
	}

	// Aks the registry for the dll path
	identifier := helper.PlatformIdentifier("notifier/portmaster-wintoast.dll")
	file, err := registry.GetFile(identifier)
	if err != nil {
		return "", err
	}
	return file.Path(), nil
}

func actionListener() {
	// initialize the library
	_ = getLib()
}
