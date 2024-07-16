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

		_, ok := notifs[state.ID]
		if !ok {
			n := notifications.Notify(&notifications.Notification{
				EventID: update.Module + ":" + state.ID,
				Type:    stateTypeToNotifType(state.Type),
				Title:   state.Name,
				Message: state.Message,
			})
			notifs[state.ID] = n
		}
	}

	// Remove notifications.
	for stateID, n := range notifs {
		if _, ok := seenStateIDs[stateID]; !ok {
			n.Delete()
			delete(notifs, stateID)
		}
	}
}

func stateTypeToNotifType(stateType mgr.StateType) notifications.Type {
	switch stateType {
	case mgr.StateTypeUndefined:
		return notifications.Info
	case mgr.StateTypeHint:
		return notifications.Info
	case mgr.StateTypeWarning:
		return notifications.Warning
	case mgr.StateTypeError:
		return notifications.Error
	default:
		return notifications.Info
	}
}
