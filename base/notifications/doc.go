/*
Package notifications provides a notification system.

# Notification Lifecycle

1. Create Notification with an ID and Message.
2. Set possible actions and save it.
3. When the user responds, the action is executed.

Example

	// create notification
	n := notifications.New("update-available", "A new update is available. Restart to upgrade.")
	// set actions and save
	n.AddAction("later", "Later").AddAction("restart", "Restart now!").Save()

	// wait for user action
	selectedAction := <-n.Response()
	switch selectedAction {
	case "later":
	  log.Infof("user wants to upgrade later.")
	case "restart":
	  log.Infof("user wants to restart now.")
	}
*/
package notifications
