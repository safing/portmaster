package notifications

import (
// "github.com/safing/portbase/modules"
// "github.com/safing/portmaster/base/log"
// "github.com/safing/portmaster/service/mgr"
)

// AttachToModule attaches the notification to a module and changes to the
// notification will be reflected on the module failure status.
// func (n *Notification) AttachToState(state *mgr.StateMgr) {
// 	if state == nil {
// 		log.Warningf("notifications: invalid usage: cannot attach %s to nil module", n.EventID)
// 		return
// 	}

// 	n.lock.Lock()
// 	defer n.lock.Unlock()

// 	if n.State != Active {
// 		log.Warningf("notifications: cannot attach module to inactive notification %s", n.EventID)
// 		return
// 	}
// 	if n.belongsTo != nil {
// 		log.Warningf("notifications: cannot override attached module for notification %s", n.EventID)
// 		return
// 	}

// 	// Attach module.
// 	n.belongsTo = state

// 	// Set module failure status.
// 	switch n.Type { //nolint:exhaustive
// 	case Info:
// 		m.Hint(n.EventID, n.Title, n.Message)
// 	case Warning:
// 		m.Warning(n.EventID, n.Title, n.Message)
// 	case Error:
// 		m.Error(n.EventID, n.Title, n.Message)
// 	default:
// 		log.Warningf("notifications: incompatible type for attaching to module in notification %s", n.EventID)
// 		m.Error(n.EventID, n.Title, n.Message+" [incompatible notification type]")
// 	}
// }

// // resolveModuleFailure removes the notification from the module failure status.
// func (n *Notification) resolveModuleFailure() {
// 	if n.belongsTo != nil {
// 		// Resolve failure in attached module.
// 		n.belongsTo.Resolve(n.EventID)

// 		// Reset attachment in order to mitigate duplicate failure resolving.
// 		// Re-attachment is prevented by the state check when attaching.
// 		n.belongsTo = nil
// 	}
// }

// func init() {
// 	modules.SetFailureUpdateNotifyFunc(mirrorModuleStatus)
// }

// func mirrorModuleStatus(moduleFailure uint8, id, title, msg string) {
// 	// Ignore "resolve all" requests.
// 	if id == "" {
// 		return
// 	}

// 	// Get notification from storage.
// 	n, ok := getNotification(id)
// 	if ok {
// 		// The notification already exists.

// 		// Check if we should delete it.
// 		if moduleFailure == modules.FailureNone && !n.Meta().IsDeleted() {

// 			// Remove belongsTo, as the deletion was already triggered by the module itself.
// 			n.Lock()
// 			n.belongsTo = nil
// 			n.Unlock()

// 			n.Delete()
// 		}

// 		return
// 	}

// 	// A notification for the given ID does not yet exists, create it.
// 	n = &Notification{
// 		EventID: id,
// 		Title:   title,
// 		Message: msg,
// 		AvailableActions: []*Action{
// 			{
// 				Text:    "Get Help",
// 				Type:    ActionTypeOpenURL,
// 				Payload: "https://safing.io/support/",
// 			},
// 		},
// 	}

// 	switch moduleFailure {
// 	case modules.FailureNone:
// 		return
// 	case modules.FailureHint:
// 		n.Type = Info
// 		n.AvailableActions = nil
// 	case modules.FailureWarning:
// 		n.Type = Warning
// 		n.ShowOnSystem = true
// 	case modules.FailureError:
// 		n.Type = Error
// 		n.ShowOnSystem = true
// 	}

// 	Notify(n)
// }
