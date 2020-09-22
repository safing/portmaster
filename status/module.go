package status

import (
	"context"

	"github.com/safing/portbase/modules"
	"github.com/safing/portmaster/netenv"
)

var (
	module *modules.Module
)

func init() {
	module = modules.Register("status", nil, start, nil, "base")
}

func start() error {
	module.StartWorker("auto-pilot", autoPilot)
	triggerAutopilot()

	err := module.RegisterEventHook(
		"netenv",
		netenv.OnlineStatusChangedEvent,
		"update online status in system status",
		func(_ context.Context, _ interface{}) error {
			triggerAutopilot()
			return nil
		},
	)
	if err != nil {
		return err
	}

	return nil
}
