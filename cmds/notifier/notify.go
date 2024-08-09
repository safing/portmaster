package main

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/safing/portmaster/base/api/client"
	"github.com/safing/portmaster/base/log"
	pbnotify "github.com/safing/portmaster/base/notifications"
	"github.com/safing/structures/dsd"
)

const (
	dbNotifBasePath = "notifications:all/"
)

var (
	notifications     = make(map[string]*Notification)
	notificationsLock sync.Mutex
)

func notifClient() {
	notifOp := apiClient.Qsub(fmt.Sprintf("query %s where ShowOnSystem is true", dbNotifBasePath), handleNotification)
	notifOp.EnableResuscitation()

	// start the action listener and block
	// until it's closed.
	actionListener()
}

func handleNotification(m *client.Message) {
	notificationsLock.Lock()
	defer notificationsLock.Unlock()

	log.Tracef("received %s msg: %s", m.Type, m.Key)

	switch m.Type {
	case client.MsgError:
	case client.MsgDone:
	case client.MsgSuccess:
	case client.MsgOk, client.MsgUpdate, client.MsgNew:

		n := &Notification{}
		_, err := dsd.Load(m.RawValue, &n.Notification)
		if err != nil {
			log.Warningf("notify: failed to parse new notification: %s", err)
			return
		}

		// copy existing system values
		existing, ok := notifications[n.EventID]
		if ok {
			existing.Lock()
			n.systemID = existing.systemID
			existing.Unlock()
		}

		// save
		notifications[n.EventID] = n

		// Handle notification.
		switch {
		case existing != nil:
			// Cancel existing notification if not active, else ignore.
			if n.State != pbnotify.Active {
				existing.Cancel()
			}
			return
		case n.State == pbnotify.Active:
			// Show new notifications that are active.
			n.Show()
		default:
			// Ignore new notifications that are not active.
		}

	case client.MsgDelete:

		n, ok := notifications[strings.TrimPrefix(m.Key, dbNotifBasePath)]
		if ok {
			n.Cancel()
			delete(notifications, n.EventID)
		}

	case client.MsgWarning:
	case client.MsgOffline:
	}
}

func clearNotifications() {
	notificationsLock.Lock()
	defer notificationsLock.Unlock()

	for _, n := range notifications {
		n.Cancel()
	}

	// Wait for goroutines that cancel notifications.
	// TODO: Revamp to use a waitgroup.
	time.Sleep(1 * time.Second)
}
