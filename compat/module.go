package compat

import (
	"context"
	"errors"
	"time"

	"github.com/tevino/abool"

	"github.com/safing/portbase/log"
	"github.com/safing/portbase/modules"
	"github.com/safing/portmaster/netenv"
	"github.com/safing/portmaster/resolver"
)

var (
	module *modules.Module

	selfcheckTask           *modules.Task
	selfcheckTaskRetryAfter = 15 * time.Second

	// selfCheckIsFailing holds whether or not the self-check is currently
	// failing. This helps other failure systems to not make noise when there is
	// an underlying failure.
	selfCheckIsFailing = abool.New()

	// selfcheckFails counts how often the self check failed successively.
	// selfcheckFails is not locked as it is only accessed by the self-check task.
	selfcheckFails int

	// selfcheckNetworkChangedFlag is used to track changed to the network for
	// the self-check.
	selfcheckNetworkChangedFlag = netenv.GetNetworkChangedFlag()
)

// selfcheckFailThreshold holds the threshold of how many times the selfcheck
// must fail before it is reported.
const selfcheckFailThreshold = 10

func init() {
	module = modules.Register("compat", prep, start, stop, "base", "network", "interception", "netenv", "notifications")

	// Workaround resolver integration.
	// See resolver/compat.go for details.
	resolver.CompatDNSCheckInternalDomainScope = DNSCheckInternalDomainScope
	resolver.CompatSelfCheckIsFailing = SelfCheckIsFailing
	resolver.CompatSubmitDNSCheckDomain = SubmitDNSCheckDomain
}

func prep() error {
	return registerAPIEndpoints()
}

func start() error {
	startNotify()

	selfcheckNetworkChangedFlag.Refresh()
	selfcheckTask = module.NewTask("compatibility self-check", selfcheckTaskFunc).
		Repeat(5 * time.Minute).
		MaxDelay(selfcheckTaskRetryAfter).
		Schedule(time.Now().Add(selfcheckTaskRetryAfter))

	module.NewTask("clean notify thresholds", cleanNotifyThreshold).
		Repeat(1 * time.Hour)

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
	// Create tracing logger.
	ctx, tracer := log.AddTracer(ctx)
	defer tracer.Submit()
	tracer.Tracef("compat: running self-check")

	// Run selfcheck and return if successful.
	issue, err := selfcheck(ctx)
	switch {
	case err == nil:
		// Successful.
		tracer.Debugf("compat: self-check successful")
	case errors.Is(err, errSelfcheckSkipped):
		// Skipped.
		tracer.Debugf("compat: %s", err)
	case issue == nil:
		// Internal error.
		tracer.Warningf("compat: %s", err)
	case selfcheckNetworkChangedFlag.IsSet():
		// The network changed, ignore the issue.
	default:
		// The self-check failed.

		// Set state and increase counter.
		selfCheckIsFailing.Set()
		selfcheckFails++

		// Log and notify.
		tracer.Errorf("compat: %s", err)
		if selfcheckFails >= selfcheckFailThreshold {
			issue.notify(err)
		}

		// Retry quicker when failed.
		task.Schedule(time.Now().Add(selfcheckTaskRetryAfter))

		return nil
	}

	// Reset self-check state.
	selfcheckNetworkChangedFlag.Refresh()
	selfCheckIsFailing.UnSet()
	selfcheckFails = 0
	resetSystemIssue()

	return nil
}

// SelfCheckIsFailing returns whether the self check is currently failing.
// This returns true after the first check fails, and does not wait for the
// failing threshold to be met.
func SelfCheckIsFailing() bool {
	return selfCheckIsFailing.IsSet()
}
