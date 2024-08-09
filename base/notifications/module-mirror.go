package notifications

import (
	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/service/mgr"
)

// SyncWithState syncs the notification to a state in the given state mgr.
// The state will be removed when the notification is removed.
func (n *Notification) SyncWithState(state *mgr.StateMgr) {
	if state == nil {
		log.Warningf("notifications: invalid usage: cannot attach %s to nil module", n.EventID)
		return
	}

	n.lock.Lock()
	defer n.lock.Unlock()

	if n.Meta().IsDeleted() {
		log.Warningf("notifications: cannot attach module to deleted notification %s", n.EventID)
		return
	}
	if n.State != Active {
		log.Warningf("notifications: cannot attach module to inactive notification %s", n.EventID)
		return
	}
	if n.belongsTo != nil {
		log.Warningf("notifications: cannot override attached module for notification %s", n.EventID)
		return
	}

	// Attach module.
	n.belongsTo = state

	// Create state with same ID.
	state.Add(mgr.State{
		ID:      n.EventID,
		Name:    n.Title,
		Message: n.Message,
		Type:    notifTypeToStateType(n.Type),
		Data:    n.EventData,
	})
}

func notifTypeToStateType(notifType Type) mgr.StateType {
	switch notifType {
	case Info:
		return mgr.StateTypeHint
	case Warning:
		return mgr.StateTypeWarning
	case Prompt:
		return mgr.StateTypeUndefined
	case Error:
		return mgr.StateTypeError
	default:
		return mgr.StateTypeUndefined
	}
}
