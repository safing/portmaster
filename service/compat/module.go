package compat

import (
	"errors"
	"sync/atomic"
	"time"

	"github.com/tevino/abool"

	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/service/mgr"
	"github.com/safing/portmaster/service/netenv"
	"github.com/safing/portmaster/service/resolver"
)

type Compat struct {
	mgr      *mgr.Manager
	instance instance
}

// Start starts the module.
func (u *Compat) Start(m *mgr.Manager) error {
	u.mgr = m
	if err := prep(); err != nil {
		return err
	}
	return start()
}

// Stop stops the module.
func (u *Compat) Stop(_ *mgr.Manager) error {
	return stop()
}

var (
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
	// module = modules.Register("compat", prep, start, stop, "base", "network", "interception", "netenv", "notifications")

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
	module.mgr.Repeat("compatibility self-check", 5*time.Minute, selfcheckTaskFunc)
	// selfcheckTask = module.NewTask("compatibility self-check", selfcheckTaskFunc).
	// 	Repeat(5 * time.Minute).
	// 	MaxDelay(selfcheckTaskRetryAfter).
	// 	Schedule(time.Now().Add(selfcheckTaskRetryAfter))

	module.mgr.Repeat("clean notify thresholds", 1*time.Hour, cleanNotifyThreshold)
	module.instance.NetEnv().EventNetworkChange.AddCallback("trigger compat self-check", func(_ *mgr.WorkerCtx, _ struct{}) (bool, error) {
		module.mgr.Delay("trigger compat self-check", selfcheckTaskRetryAfter, selfcheckTaskFunc)
		return false, nil
	})
	return nil
}

func stop() error {
	// selfcheckTask.Cancel()
	// selfcheckTask = nil

	return nil
}

func selfcheckTaskFunc(wc *mgr.WorkerCtx) error {
	// Create tracing logger.
	ctx, tracer := log.AddTracer(wc.Ctx())
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
		module.mgr.Delay("trigger compat self-check", selfcheckTaskRetryAfter, selfcheckTaskFunc)
		// task.Schedule(time.Now().Add(selfcheckTaskRetryAfter))

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

var (
	module     *Compat
	shimLoaded atomic.Bool
)

// New returns a new Compat module.
func New(instance instance) (*Compat, error) {
	if !shimLoaded.CompareAndSwap(false, true) {
		return nil, errors.New("only one instance allowed")
	}

	module = &Compat{
		instance: instance,
	}
	return module, nil
}

type instance interface {
	NetEnv() *netenv.NetEnv
}
