package compat

import (
	"context"
	"time"

	"github.com/safing/portbase/log"
	"github.com/safing/portbase/modules"
	"github.com/safing/portmaster/netenv"
	"github.com/tevino/abool"
)

var (
	module *modules.Module

	selfcheckTask           *modules.Task
	selfcheckTaskRetryAfter = 10 * time.Second

	// selfCheckIsFailing holds whether or not the self-check is currently
	// failing. This helps other failure systems to not make noise when there is
	// an underlying failure.
	selfCheckIsFailing = abool.New()

	// selfcheckFails counts how often the self check failed successively.
	// selfcheckFails is not locked as it is only accessed by the self-check task.
	selfcheckFails int
)

func init() {
	module = modules.Register("compat", prep, start, stop, "base", "network", "interception", "netenv", "notifications")
}

func prep() error {
	return registerAPIEndpoints()
}

func start() error {
	selfcheckTask = module.NewTask("compatibility self-check", selfcheckTaskFunc).
		Repeat(1 * time.Minute).
		MaxDelay(selfcheckTaskRetryAfter).
		Schedule(time.Now().Add(selfcheckTaskRetryAfter))

	return module.RegisterEventHook(
		netenv.ModuleName,
		netenv.NetworkChangedEvent,
		"trigger compat self-check",
		func(_ context.Context, _ interface{}) error {
			selfcheckTask.Schedule(time.Now().Add(selfcheckTaskRetryAfter))
			return nil
		},
	)
}

func stop() error {
	selfcheckTask.Cancel()
	selfcheckTask = nil

	return nil
}

func selfcheckTaskFunc(ctx context.Context, task *modules.Task) error {
	// Run selfcheck and return if successful.
	issue, err := selfcheck(ctx)
	if err == nil {
		selfCheckIsFailing.UnSet()
		selfcheckFails = 0
		resetSystemIssue()
		return nil
	}

	// Log result.
	if issue != nil {
		selfCheckIsFailing.Set()
		selfcheckFails++

		log.Errorf("compat: %s", err)
		if selfcheckFails >= 3 {
			issue.notify(err)
		}

		// Retry quicker when failed.
		task.Schedule(time.Now().Add(selfcheckTaskRetryAfter))
	} else {
		selfCheckIsFailing.UnSet()
		selfcheckFails = 0

		// Only log internal errors, but don't notify.
		log.Warningf("compat: %s", err)
	}

	return nil
}

func SelfCheckIsFailing() bool {
	return selfCheckIsFailing.IsSet()
}
