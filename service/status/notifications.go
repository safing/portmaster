package status

import (
	"github.com/safing/portmaster/base/notifications"
	"github.com/safing/portmaster/service/mgr"
)

func (s *Status) deriveNotificationsFromStateUpdate(update mgr.StateUpdate) {
	s.notificationsLock.Lock()
	defer s.notificationsLock.Unlock()

	notifs := s.notifications[update.Module]
	if notifs == nil {
		notifs = make(map[string]*notifications.Notification)
		s.notifications[update.Module] = notifs
	}

	// Add notifications.
	seenStateIDs := make(map[string]struct{}, len(update.States))
	for _, state := range update.States {
		seenStateIDs[state.ID] = struct{}{}

		// Check if we already have a notification registered.
		if _, ok := notifs[state.ID]; ok {
			continue
		}

		// Check if the notification was pre-created.
		// If a matching notification is found, assign it.
		n := notifications.Get(state.ID)
		if n != nil {
			notifs[state.ID] = n
			continue
		}

		// Create a new notification.
		n = &notifications.Notification{
			EventID: state.ID,
			Title:   state.Name,
			Message: state.Message,
			AvailableActions: []*notifications.Action{
				{
					Text:    "Get Help",
					Type:    notifications.ActionTypeOpenURL,
					Payload: "https://safing.io/support/",
				},
			},
		}
		switch state.Type {
		case mgr.StateTypeWarning:
			n.Type = notifications.Warning
			n.ShowOnSystem = true
		case mgr.StateTypeError:
			n.Type = notifications.Error
			n.ShowOnSystem = true
		case mgr.StateTypeHint, mgr.StateTypeUndefined:
			fallthrough
		default:
			n.Type = notifications.Info
			n.AvailableActions = nil
		}

		notifs[state.ID] = n
		notifications.Notify(n)
	}

	// Remove notifications.
	for stateID, n := range notifs {
		if _, ok := seenStateIDs[stateID]; !ok {
			n.Delete()
			delete(notifs, stateID)
		}
	}
}
