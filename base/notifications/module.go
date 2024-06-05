package notifications

import (
	"fmt"
	"time"

	"github.com/safing/portmaster/base/config"
	"github.com/safing/portmaster/base/modules"
)

var module *modules.Module

func init() {
	module = modules.Register("notifications", prep, start, nil, "database", "config", "base")
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

	go module.StartServiceWorker("cleaner", 1*time.Second, cleaner)
	return nil
}

func showConfigLoadingErrors() {
	validationErrors := config.GetLoadedConfigValidationErrors()
	if len(validationErrors) == 0 {
		return
	}

	// Trigger a module error for more awareness.
	module.Error(
		"config:validation-errors-on-load",
		"Invalid Settings",
		"Some current settings are invalid. Please update them and restart the Portmaster.",
	)

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
