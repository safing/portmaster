package resolver

import (
	"time"

	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/service/mgr"
	"github.com/safing/portmaster/service/netenv"
)

var (
	// FailThreshold is amount of errors a resolvers must experience in order to be regarded as failed.
	FailThreshold = 5

	// FailObserveDuration is the duration in which failures are counted in order to mark a resolver as failed.
	FailObserveDuration = time.Duration(FailThreshold) * 10 * time.Second
)

// IsFailing returns if this resolver is currently failing.
func (brc *BasicResolverConn) IsFailing() bool {
	return brc.failing.IsSet()
}

// ReportFailure reports that an error occurred with this resolver.
func (brc *BasicResolverConn) ReportFailure() {
	// Don't mark resolver as failed if we are offline.
	if !netenv.Online() {
		return
	}

	// Ingore report when we are already failing.
	if brc.IsFailing() {
		return
	}

	brc.failLock.Lock()
	defer brc.failLock.Unlock()

	// Check if we are within the observation period.
	if time.Since(brc.failingStarted) > FailObserveDuration {
		brc.fails = 1
		brc.failingStarted = time.Now()
		return
	}

	// Increase and check if we need to set to failing.
	brc.fails++
	if brc.fails > FailThreshold {
		brc.failing.Set()
	}

	// Report to netenv that a configured server failed.
	if brc.resolver.Info.Source == ServerSourceConfigured {
		netenv.ConnectedToDNS.UnSet()
	}
}

// ResetFailure resets the failure status.
func (brc *BasicResolverConn) ResetFailure() {
	if brc.failing.SetToIf(true, false) {
		brc.failLock.Lock()
		defer brc.failLock.Unlock()
		brc.fails = 0
		brc.failingStarted = time.Time{}
	}

	// Report to netenv that a configured server succeeded.
	if brc.resolver.Info.Source == ServerSourceConfigured {
		netenv.ConnectedToDNS.Set()
	}
}

func checkFailingResolvers(wc *mgr.WorkerCtx) error {
	var resolvers []*Resolver

	// Set next execution time.
	module.failingResolverWorkerMgr.Delay(time.Duration(nameserverRetryRate()) * time.Second)

	// Make a copy of the resolver list.
	func() {
		resolversLock.Lock()
		defer resolversLock.Unlock()

		resolvers = make([]*Resolver, len(globalResolvers))
		copy(resolvers, globalResolvers)
	}()

	// Start logging.
	ctx, tracer := log.AddTracer(wc.Ctx())
	tracer.Debugf("resolver: checking failed resolvers")
	defer tracer.Submit()

	// Go through all resolvers and check if they are reachable again.
	for i, resolver := range resolvers {
		// Skip resolver that are not failing.
		if !resolver.Conn.IsFailing() {
			continue
		}

		tracer.Tracef("resolver: testing failed resolver [%d/%d] %s", i+1, len(resolvers), resolver)

		// Test if we can resolve via this resolver.
		ips, _, err := testConnectivity(ctx, netenv.DNSTestDomain, resolver)
		switch {
		case err != nil:
			tracer.Debugf("resolver: failed resolver %s is still failing: %s", resolver, err)
		case len(ips) == 0 || !ips[0].Equal(netenv.DNSTestExpectedIP):
			tracer.Debugf("resolver: failed resolver %s received unexpected A records: %s", resolver, ips)
		default:
			// Resolver test successful.
			tracer.Infof("resolver: check successful, resolver %s is available again", resolver)
			resolver.Conn.ResetFailure()
		}

		// Check if context was canceled.
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}

	return nil
}
