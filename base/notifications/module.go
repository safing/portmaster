package notifications

import (
	"errors"
	"fmt"
	"sync/atomic"

	"github.com/safing/portmaster/base/config"
	"github.com/safing/portmaster/service/mgr"
)

type Notifications struct {
	mgr      *mgr.Manager
	instance instance

	states *mgr.StateMgr
}

func (n *Notifications) Manager() *mgr.Manager {
	return n.mgr
}

func (n *Notifications) States() *mgr.StateMgr {
	return n.states
}

func (n *Notifications) Start() error {
	return start()
}

func (n *Notifications) Stop() error {
	return nil
}

// NotifyInfo is a helper method for quickly showing an info notification.
// The notification will be activated immediately.
// If the provided id is empty, an id will derived from msg.
// ShowOnSystem is disabled.
// If no actions are defined, a default "OK" (ID:"ack") action will be added.
func (n *Notifications) NotifyInfo(id, title, msg string, actions ...Action) *Notification {
	return NotifyInfo(id, title, msg, actions...)
}

// NotifyWarn is a helper method for quickly showing a warning notification
// The notification will be activated immediately.
// If the provided id is empty, an id will derived from msg.
// ShowOnSystem is enabled.
// If no actions are defined, a default "OK" (ID:"ack") action will be added.
func (n *Notifications) NotifyWarn(id, title, msg string, actions ...Action) *Notification {
	return NotifyWarn(id, title, msg, actions...)
}

// NotifyError is a helper method for quickly showing an error notification.
// The notification will be activated immediately.
// If the provided id is empty, an id will derived from msg.
// ShowOnSystem is enabled.
// If no actions are defined, a default "OK" (ID:"ack") action will be added.
func (n *Notifications) NotifyError(id, title, msg string, actions ...Action) *Notification {
	return NotifyError(id, title, msg, actions...)
}

// NotifyPrompt is a helper method for quickly showing a prompt notification.
// The notification will be activated immediately.
// If the provided id is empty, an id will derived from msg.
// ShowOnSystem is disabled.
// If no actions are defined, a default "OK" (ID:"ack") action will be added.
func (n *Notifications) NotifyPrompt(id, title, msg string, actions ...Action) *Notification {
	return NotifyPrompt(id, title, msg, actions...)
}

// Notify sends the given notification.
func (n *Notifications) Notify(notification *Notification) *Notification {
	return Notify(notification)
}

func prep() error {
	return registerConfig()
}

func start() error {
	err := registerAsDatabase()
	if err != nil {
		return err
	}

	showConfigLoadingErrors()

	module.mgr.Go("cleaner", cleaner)
	return nil
}

func showConfigLoadingErrors() {
	validationErrors := config.GetLoadedConfigValidationErrors()
	if len(validationErrors) == 0 {
		return
	}

	// Trigger a module error for more awareness.
	module.states.Add(mgr.State{
		ID:      "config:validation-errors-on-load",
		Name:    "无效设置",
		Message: "当前部分设置无效。请更新这些设置并重启 Portmaster。",
		Type:    mgr.StateTypeError,
	})

	// Send one notification per invalid setting.
	for _, validationError := range config.GetLoadedConfigValidationErrors() {
		NotifyError(
			fmt.Sprintf("config:validation-error:%s", validationError.Option.Key),
			fmt.Sprintf("%s 的设置无效", validationError.Option.Name),
			fmt.Sprintf(`您当前对 %s 的设置无效：%s

请更新该设置并重启 Portmaster，在此之前将使用默认值。`,
				validationError.Option.Name,
				validationError.Err.Error(),
			),
			Action{
				Text: "修改",
				Type: ActionTypeOpenSetting,
				Payload: &ActionTypeOpenSettingPayload{
					Key: validationError.Option.Key,
				},
			},
		)
	}
}

var (
	module     *Notifications
	shimLoaded atomic.Bool
)

func New(instance instance) (*Notifications, error) {
	if !shimLoaded.CompareAndSwap(false, true) {
		return nil, errors.New("only one instance allowed")
	}
	m := mgr.New("Notifications")
	module = &Notifications{
		mgr:      m,
		instance: instance,

		states: mgr.NewStateMgr(m),
	}

	if err := prep(); err != nil {
		return nil, err
	}

	return module, nil
}

type instance interface{}
