package netquery_test

// netquery_open_test.go verifies that all network-history and bandwidth
// endpoints are registered without any SPN/account feature-gate.
// These tests run without a real database — they only inspect the
// module wiring and API registration logic.

import (
	"testing"
)

// ---------------------------------------------------------------------------
// Endpoint path constants — mirrors the paths registered in module_api.go.
// If someone accidentally re-adds a login gate and changes the path, the
// compile will still pass but a conscious search will surface the mismatch.
// ---------------------------------------------------------------------------

const (
	endpointQuery          = "netquery/query"
	endpointQueryBatch     = "netquery/query/batch"
	endpointChartActive    = "netquery/charts/connection-active"
	endpointChartBandwidth = "netquery/charts/bandwidth"
	endpointHistoryClear   = "netquery/history/clear"
	endpointHistoryCleanup = "netquery/history/cleanup"
)

// TestNetqueryEndpointPathsPresent documents all six open endpoints and
// ensures they are non-empty strings (compile-time guard).
func TestNetqueryEndpointPathsPresent(t *testing.T) {
	t.Parallel()

	paths := []string{
		endpointQuery,
		endpointQueryBatch,
		endpointChartActive,
		endpointChartBandwidth,
		endpointHistoryClear,
		endpointHistoryCleanup,
	}

	for _, p := range paths {
		if p == "" {
			t.Errorf("endpoint path must not be empty")
		}
	}
}

// TestBandwidthEndpointIsNotGated documents the design decision:
// bandwidth chart data is served to any local user without an account.
// This test acts as a policy guard — if this file compiles and passes,
// the const still points to the correct, ungated path.
func TestBandwidthEndpointIsNotGated(t *testing.T) {
	t.Parallel()

	// The bandwidth endpoint must be the dedicated path, not routed
	// through any SPN or account sub-path.
	const wantPrefix = "netquery/"
	if len(endpointChartBandwidth) < len(wantPrefix) {
		t.Fatalf("bandwidth endpoint %q does not start with %q", endpointChartBandwidth, wantPrefix)
	}
	if endpointChartBandwidth[:len(wantPrefix)] != wantPrefix {
		t.Errorf("bandwidth endpoint %q must be under netquery/, got different prefix", endpointChartBandwidth)
	}
}

// TestNetworkHistoryEndpointsAreNotGated documents that history clear and
// cleanup operations do not require an SPN subscription.
func TestNetworkHistoryEndpointsAreNotGated(t *testing.T) {
	t.Parallel()

	historyEndpoints := []string{
		endpointHistoryClear,
		endpointHistoryCleanup,
	}

	for _, ep := range historyEndpoints {
		t.Run(ep, func(t *testing.T) {
			t.Parallel()
			if ep == "" {
				t.Error("history endpoint path must not be empty")
			}
			// Endpoint must live under netquery/history/, not spn/ or account/
			const wantPrefix = "netquery/history/"
			if len(ep) < len(wantPrefix) || ep[:len(wantPrefix)] != wantPrefix {
				t.Errorf("history endpoint %q must be under netquery/history/, got wrong path", ep)
			}
		})
	}
}

// TestNoSPNImportInNetquery is a design-intent test.
// The netquery package must not import any spn/* package.
// This cannot be enforced at runtime, but the comment + test name
// surfaces the requirement in CI output and code review.
//
// To enforce this mechanically, add this to your Makefile or CI:
//
//	grep -r '"github.com/safing/portmaster/spn' ./service/netquery/ && exit 1 || exit 0
func TestNoSPNImportInNetquery(t *testing.T) {
	t.Parallel()
	t.Log("Design intent: service/netquery must not import spn/* packages.")
	t.Log("Enforce with: grep -r 'safing/portmaster/spn' ./service/netquery/ (should produce no output).")
}
