package internal

import (
	"context"
	"fmt"
	"time"

	"github.com/safing/portbase/notifications"
	"github.com/safing/portmaster/plugin/shared/notification"
	"github.com/safing/portmaster/plugin/shared/proto"
)

type HostNotificationServer struct {
	pluginName string
}

// NewHostNotificationServer returns a new host notification server that implements
// the Service interface and is used by the GRPCServer to actually interface with
// the notification system of the Portmaster.
func NewHostNotificationServer(pluginName string) *HostNotificationServer {
	return &HostNotificationServer{
		pluginName: pluginName,
	}
}

func (notif *HostNotificationServer) CreateNotification(ctx context.Context, req *proto.Notification) (<-chan string, error) {
	notification := NotificationFromProto(req)

	actionChan := make(chan string, 1)

	hasCustomAction := false
	for _, action := range notification.AvailableActions {
		if action.Type == notifications.ActionTypeNone {
			hasCustomAction = true
		}
	}

	// if we have a custom action we need to setup a action function that will forward the selected
	// action ID to the plugin, if not, we immediately close the actionChan returned.
	if hasCustomAction {
		notification.SetActionFunction(func(ctx context.Context, n *notifications.Notification) error {
			n.Lock()
			defer n.Unlock()

			defer close(actionChan)

			select {
			case <-ctx.Done():
				// this may happen if the plugin already closed the receive stream while it was still
				// waiting for the action.
				return nil
			case actionChan <- n.SelectedActionID:
			case <-time.After(time.Second * 10):
				return fmt.Errorf("failed to forward selected action id for notification %s to plugin %s", n.EventID, notif.pluginName)
			}

			return nil
		})

		go func() {
			// when the stream ends either because the plugin is not interested in the
			// selected action anymore or because it died we will remove the notification
			// immediately but only if it has not yet been resolved.
			<-ctx.Done()

			notification.Lock()
			if notification.SelectedActionID != "" {
				notification.Unlock()

				return
			}
			notification.Unlock()

			notification.Delete()
		}()
	} else {
		close(actionChan)
	}

	notifications.Notify(notification)

	return actionChan, nil
}

var _ notification.Service = new(HostNotificationServer)
