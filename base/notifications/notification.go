package notifications

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/safing/portmaster/base/database/record"
	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/base/utils"
	"github.com/safing/portmaster/service/mgr"
)

// Type describes the type of a notification.
type Type uint8

// Notification types.
const (
	Info    Type = 0
	Warning Type = 1
	Prompt  Type = 2
	Error   Type = 3
)

// State describes the state of a notification.
type State string

// NotificationActionFn defines the function signature for notification action
// functions.
type NotificationActionFn func(context.Context, *Notification) error

// Possible notification states.
// State transitions can only happen from top to bottom.
const (
	// Active describes a notification that is active, no expired and,
	// if actions are available, still waits for the user to select an
	// action.
	Active State = "active"
	// Responded describes a notification where the user has already
	// selected which action to take but that action is still to be
	// performed.
	Responded State = "responded"
	// Executes describes a notification where the user has selected
	// and action and that action has been performed.
	Executed State = "executed"
)

// Notification represents a notification that is to be delivered to the user.
type Notification struct { //nolint:maligned
	record.Base
	// EventID is used to identify a specific notification. It consists of
	// the module name and a per-module unique event id.
	// The following format is recommended:
	// 	<module-id>:<event-id>
	EventID string
	// GUID is a unique identifier for each notification instance. That is
	// two notifications with the same EventID must still have unique GUIDs.
	// The GUID is mainly used for system (Windows) integration and is
	// automatically populated by the notification package. Average users
	// don't need to care about this field.
	GUID string
	// Type is the notification type. It can be one of Info, Warning or Prompt.
	Type Type
	// Title is an optional and very short title for the message that gives a
	// hint about what the notification is about.
	Title string
	// Category is an optional category for the notification that allows for
	// tagging and grouping notifications by category.
	Category string
	// Message is the default message shown to the user if no localized version
	// of the notification is available. Note that the message should already
	// have any paramerized values replaced.
	Message string
	// ShowOnSystem specifies if the notification should be also shown on the
	// operating system. Notifications shown on the operating system level are
	// more focus-intrusive and should only be used for important notifications.
	// If the configuration option "Desktop Notifications" is switched off, this
	// will be forced to false on the first save.
	ShowOnSystem bool
	// EventData contains an additional payload for the notification. This payload
	// may contain contextual data and may be used by a localization framework
	// to populate the notification message template.
	// If EventData implements sync.Locker it will be locked and unlocked together with the
	// notification. Otherwise, EventData is expected to be immutable once the
	// notification has been saved and handed over to the notification or database package.
	EventData interface{}
	// Expires holds the unix epoch timestamp at which the notification expires
	// and can be cleaned up.
	// Users can safely ignore expired notifications and should handle expiry the
	// same as deletion.
	Expires int64
	// State describes the current state of a notification. See State for
	// a list of available values and their meaning.
	State State
	// AvailableActions defines a list of actions that a user can choose from.
	AvailableActions []*Action
	// SelectedActionID is updated to match the ID of one of the AvailableActions
	// based on the user selection.
	SelectedActionID string

	// belongsTo holds the state this notification belongs to. The notification
	// lifecycle will be mirrored to the specified failure status.
	belongsTo *mgr.StateMgr

	lock           sync.Mutex
	actionFunction NotificationActionFn // call function to process action
	actionTrigger  chan string          // and/or send to a channel
	expiredTrigger chan struct{}        // closed on expire
}

// Action describes an action that can be taken for a notification.
type Action struct {
	// ID specifies a unique ID for the action. If an action is selected, the ID
	// is written to SelectedActionID and the notification is saved.
	// If the action type is not ActionTypeNone, the ID may be empty, signifying
	// that this action is merely additional and selecting it does not dismiss the
	// notification.
	ID string
	// Text on the button.
	Text string
	// Type specifies the action type. Implementing interfaces should only
	// display action types they can handle.
	Type ActionType
	// Payload holds additional data for special action types.
	Payload interface{}
}

// ActionType defines a specific type of action.
type ActionType string

// Action Types.
const (
	ActionTypeNone        = ""             // Report selected ID back to backend.
	ActionTypeOpenURL     = "open-url"     // Open external URL
	ActionTypeOpenPage    = "open-page"    // Payload: Page ID
	ActionTypeOpenSetting = "open-setting" // Payload: See struct definition below.
	ActionTypeOpenProfile = "open-profile" // Payload: Scoped Profile ID
	ActionTypeInjectEvent = "inject-event" // Payload: Event ID
	ActionTypeWebhook     = "call-webhook" // Payload: See struct definition below.
)

// ActionTypeOpenSettingPayload defines the payload for the OpenSetting Action Type.
type ActionTypeOpenSettingPayload struct {
	// Key is the key of the setting.
	Key string
	// Profile is the scoped ID of the profile.
	// Leaving this empty opens the global settings.
	Profile string
}

// ActionTypeWebhookPayload defines the payload for the WebhookPayload Action Type.
type ActionTypeWebhookPayload struct {
	// HTTP Method to use. Defaults to "GET", or "POST" if a Payload is supplied.
	Method string
	// URL to call.
	// If the URL is relative, prepend the current API endpoint base path.
	// If the URL is absolute, send request to the Portmaster.
	URL string
	// Payload holds arbitrary payload data.
	Payload interface{}
	// ResultAction defines what should be done with successfully returned data.
	// Must one of:
	// - `ignore`: do nothing (default)
	// - `display`: the result is a human readable message, display it in a success message.
	ResultAction string
}

// Get returns the notification identifed by the given id or nil if it doesn't exist.
func Get(id string) *Notification {
	notsLock.RLock()
	defer notsLock.RUnlock()
	n, ok := nots[id]
	if ok {
		return n
	}
	return nil
}

// Delete deletes the notification with the given id.
func Delete(id string) {
	// Delete notification in defer to enable deferred unlocking.
	var n *Notification
	var ok bool
	defer func() {
		if ok {
			n.Delete()
		}
	}()

	notsLock.Lock()
	defer notsLock.Unlock()
	n, ok = nots[id]
}

// NotifyInfo is a helper method for quickly showing an info notification.
// The notification will be activated immediately.
// If the provided id is empty, an id will derived from msg.
// ShowOnSystem is disabled.
// If no actions are defined, a default "OK" (ID:"ack") action will be added.
func NotifyInfo(id, title, msg string, actions ...Action) *Notification {
	return notify(Info, id, title, msg, false, actions...)
}

// NotifyWarn is a helper method for quickly showing a warning notification
// The notification will be activated immediately.
// If the provided id is empty, an id will derived from msg.
// ShowOnSystem is enabled.
// If no actions are defined, a default "OK" (ID:"ack") action will be added.
func NotifyWarn(id, title, msg string, actions ...Action) *Notification {
	return notify(Warning, id, title, msg, true, actions...)
}

// NotifyError is a helper method for quickly showing an error notification.
// The notification will be activated immediately.
// If the provided id is empty, an id will derived from msg.
// ShowOnSystem is enabled.
// If no actions are defined, a default "OK" (ID:"ack") action will be added.
func NotifyError(id, title, msg string, actions ...Action) *Notification {
	return notify(Error, id, title, msg, true, actions...)
}

// NotifyPrompt is a helper method for quickly showing a prompt notification.
// The notification will be activated immediately.
// If the provided id is empty, an id will derived from msg.
// ShowOnSystem is disabled.
// If no actions are defined, a default "OK" (ID:"ack") action will be added.
func NotifyPrompt(id, title, msg string, actions ...Action) *Notification {
	return notify(Prompt, id, title, msg, false, actions...)
}

func notify(nType Type, id, title, msg string, showOnSystem bool, actions ...Action) *Notification {
	// Process actions.
	var acts []*Action
	if len(actions) == 0 {
		// Create ack action if there are no defined actions.
		acts = []*Action{
			{
				ID:   "ack",
				Text: "OK",
			},
		}
	} else {
		// Reference given actions for notification.
		acts = make([]*Action, len(actions))
		for index := range actions {
			a := actions[index]
			acts[index] = &a
		}
	}

	return Notify(&Notification{
		EventID:          id,
		Type:             nType,
		Title:            title,
		Message:          msg,
		ShowOnSystem:     showOnSystem,
		AvailableActions: acts,
	})
}

// Notify sends the given notification.
func Notify(n *Notification) *Notification {
	// While this function is very similar to Save(), it is much nicer to use in
	// order to just fire off one notification, as it does not require some more
	// uncommon Go syntax.

	n.save(true)
	return n
}

// Save saves the notification.
func (n *Notification) Save() {
	n.save(true)
}

// save saves the notification to the internal storage. It locks the
// notification, so it must not be locked when save is called.
func (n *Notification) save(pushUpdate bool) {
	var id string

	// Save notification after pre-save processing.
	defer func() {
		if id != "" {
			// Lock and save to notification storage.
			notsLock.Lock()
			defer notsLock.Unlock()
			nots[id] = n
		}
	}()

	// We do not access EventData here, so it is enough to just lock the
	// notification itself.
	n.lock.Lock()
	defer n.lock.Unlock()

	// Check if required data is present.
	if n.Title == "" && n.Message == "" {
		log.Warning("notifications: ignoring notification without Title or Message")
		return
	}

	// Derive EventID from Message if not given.
	if n.EventID == "" {
		n.EventID = fmt.Sprintf(
			"unknown:%s",
			utils.DerivedInstanceUUID(n.Message).String(),
		)
	}

	// Save ID for deletion
	id = n.EventID

	// Generate random GUID if not set.
	if n.GUID == "" {
		n.GUID = utils.RandomUUID(n.EventID).String()
	}

	// Make sure we always have a notification state assigned.
	if n.State == "" {
		n.State = Active
	}

	// Initialize on first save.
	if !n.KeyIsSet() {
		// Set database key.
		n.SetKey(fmt.Sprintf("notifications:all/%s", n.EventID))

		// Check if notifications should be shown on the system at all.
		if !useSystemNotifications() {
			n.ShowOnSystem = false
		}
	}

	// Update meta data.
	n.UpdateMeta()

	// Push update via the database system if needed.
	if pushUpdate {
		log.Tracef("notifications: pushing update for %s to subscribers", n.Key())
		dbController.PushUpdate(n)
	}
}

// SetActionFunction sets a trigger function to be executed when the user reacted on the notification.
// The provided function will be started as its own goroutine and will have to lock everything it accesses, even the provided notification.
func (n *Notification) SetActionFunction(fn NotificationActionFn) *Notification {
	n.lock.Lock()
	defer n.lock.Unlock()
	n.actionFunction = fn
	return n
}

// Response waits for the user to respond to the notification and returns the selected action.
func (n *Notification) Response() <-chan string {
	n.lock.Lock()
	defer n.lock.Unlock()

	if n.actionTrigger == nil {
		n.actionTrigger = make(chan string)
	}

	return n.actionTrigger
}

// Update updates/resends a notification if it was not already responded to.
func (n *Notification) Update(expires int64) {
	// Save when we're finished, if needed.
	save := false
	defer func() {
		if save {
			n.save(true)
		}
	}()

	n.lock.Lock()
	defer n.lock.Unlock()

	// Don't update if notification isn't active.
	if n.State != Active {
		return
	}

	// Don't update too quickly.
	if n.Meta().Modified > time.Now().Add(-10*time.Second).Unix() {
		return
	}

	// Update expiry and save.
	n.Expires = expires
	save = true
}

// Delete (prematurely) cancels and deletes a notification.
func (n *Notification) Delete() {
	// Dismiss notification.
	func() {
		n.lock.Lock()
		defer n.lock.Unlock()

		if n.actionTrigger != nil {
			close(n.actionTrigger)
			n.actionTrigger = nil
		}
	}()

	n.delete(true)
}

// delete deletes the notification from the internal storage. It locks the
// notification, so it must not be locked when delete is called.
func (n *Notification) delete(pushUpdate bool) {
	var id string

	// Delete notification after processing deletion.
	defer func() {
		// Lock and delete from notification storage.
		notsLock.Lock()
		defer notsLock.Unlock()
		delete(nots, id)
	}()

	// We do not access EventData here, so it is enough to just lock the
	// notification itself.
	n.lock.Lock()
	defer n.lock.Unlock()

	// Check if notification is already deleted.
	if n.Meta().IsDeleted() {
		return
	}

	// Save ID for deletion
	id = n.EventID

	// Mark notification as deleted.
	n.Meta().Delete()

	// Close expiry channel if available.
	if n.expiredTrigger != nil {
		close(n.expiredTrigger)
		n.expiredTrigger = nil
	}

	// Push update via the database system if needed.
	if pushUpdate {
		dbController.PushUpdate(n)
	}

	// Remove the connected state.
	if n.belongsTo != nil {
		n.belongsTo.Remove(n.EventID)
	}
}

// Expired notifies the caller when the notification has expired.
func (n *Notification) Expired() <-chan struct{} {
	n.lock.Lock()
	defer n.lock.Unlock()

	if n.expiredTrigger == nil {
		n.expiredTrigger = make(chan struct{})
	}

	return n.expiredTrigger
}

// selectAndExecuteAction sets the user response and executes/triggers the action, if possible.
func (n *Notification) selectAndExecuteAction(id string) {
	if n.State != Active {
		return
	}

	n.State = Responded
	n.SelectedActionID = id

	executed := false
	if n.actionFunction != nil {
		module.mgr.Go("notification action execution", func(ctx *mgr.WorkerCtx) error {
			return n.actionFunction(ctx.Ctx(), n)
		})
		executed = true
	}

	if n.actionTrigger != nil {
		// satisfy all listeners (if they are listening)
		// TODO(ppacher): if we miss to notify the waiter here (because
		//                nobody is listeing on actionTrigger) we wil likely
		//                never be able to execute the action again (simply because
		//                we won't try). May consider replacing the single actionTrigger
		//                channel with a per-listener (buffered) one so we just send
		//                the value and close the channel.
	triggerAll:
		for {
			select {
			case n.actionTrigger <- n.SelectedActionID:
				executed = true
			case <-time.After(100 * time.Millisecond): // mitigate race conditions
				break triggerAll
			}
		}
	}

	if executed {
		n.State = Executed
		// n.resolveModuleFailure()
	}
}

// Lock locks the Notification. If EventData is set and
// implements sync.Locker it is locked as well. Users that
// want to replace the EventData on a notification must
// ensure to unlock the current value on their own. If the
// new EventData implements sync.Locker as well, it must
// be locked prior to unlocking the notification.
func (n *Notification) Lock() {
	n.lock.Lock()
	if locker, ok := n.EventData.(sync.Locker); ok {
		locker.Lock()
	}
}

// Unlock unlocks the Notification and the EventData, if
// it implements sync.Locker. See Lock() for more information
// on how to replace and work with EventData.
func (n *Notification) Unlock() {
	n.lock.Unlock()
	if locker, ok := n.EventData.(sync.Locker); ok {
		locker.Unlock()
	}
}
