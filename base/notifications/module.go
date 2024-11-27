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
	if err := prep(); err != nil {
		return err
	}

	return start()
}

func (n *Notifications) Stop() error {
	return nil
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
		Name:    "Invalid Settings",
		Message: "Some current settings are invalid. Please update them and restart the Portmaster.",
		Type:    mgr.StateTypeError,
	})

	// Send one notification per invalid setting.
	for _, validationError := range config.GetLoadedConfigValidationErrors() {
		NotifyError(
			fmt.Sprintf("config:validation-error:%s", validationError.Option.Key),
			fmt.Sprintf("Invalid Setting for %s", validationError.Option.Name),
			fmt.Sprintf(`Your current setting for %s is invalid: %s

Please update the setting and restart the Portmaster, until then the default value is used.`,
				validationError.Option.Name,
				validationError.Err.Error(),
			),
			Action{
				Text: "Change",
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

	return module, nil
}

type instance interface{}
