package patrol

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/tevino/abool"

	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/service/mgr"
	"github.com/safing/portmaster/spn/conf"
)

var httpsConnectivityConfirmed = abool.NewBool(true)

// HTTPSConnectivityConfirmed returns whether the last HTTPS connectivity check succeeded.
// Is "true" before first test.
func HTTPSConnectivityConfirmed() bool {
	return httpsConnectivityConfirmed.IsSet()
}

func connectivityCheckTask(wc *mgr.WorkerCtx) error {
	// Start tracing logs.
	ctx, tracer := log.AddTracer(wc.Ctx())
	defer tracer.Submit()

	// Run checks and report status.
	success := runConnectivityChecks(ctx)
	if success {
		tracer.Info("spn/patrol: all connectivity checks succeeded")
		if httpsConnectivityConfirmed.SetToIf(false, true) {
			module.EventChangeSignal.Submit(struct{}{})
		}
		return nil
	}

	tracer.Errorf("spn/patrol: connectivity check failed")
	if httpsConnectivityConfirmed.SetToIf(true, false) {
		module.EventChangeSignal.Submit(struct{}{})
	}
	return nil
}

func runConnectivityChecks(ctx context.Context) (ok bool) {
	switch {
	case conf.HubHasIPv4() && !runHTTPSConnectivityChecks(ctx, "tcp4"):
		return false
	case conf.HubHasIPv6() && !runHTTPSConnectivityChecks(ctx, "tcp6"):
		return false
	default:
		// All checks passed.
		return true
	}
}

func runHTTPSConnectivityChecks(ctx context.Context, network string) (ok bool) {
	// Step 1: Check 1 domain, require 100%
	if checkHTTPSConnectivity(ctx, network, 1, 1) {
		return true
	}

	// Step 2: Check 5 domains, require 80%
	if checkHTTPSConnectivity(ctx, network, 5, 0.8) {
		return true
	}

	// Step 3: Check 20 domains, require 70%
	if checkHTTPSConnectivity(ctx, network, 20, 0.7) {
		return true
	}

	return false
}

func checkHTTPSConnectivity(ctx context.Context, network string, checks int, requiredSuccessFraction float32) (ok bool) {
	log.Tracer(ctx).Tracef(
		"spn/patrol: testing connectivity via https (%d checks; %.0f%% required)",
		checks,
		requiredSuccessFraction*100,
	)

	// Run tests.
	var succeeded int
	for range checks {
		if checkHTTPSConnection(ctx, network) {
			succeeded++
		}
	}

	// Check success.
	successFraction := float32(succeeded) / float32(checks)
	if successFraction < requiredSuccessFraction {
		log.Tracer(ctx).Warningf(
			"spn/patrol: https/%s connectivity check failed: %d/%d (%.0f%%)",
			network,
			succeeded,
			checks,
			successFraction*100,
		)
		return false
	}

	log.Tracer(ctx).Debugf(
		"spn/patrol: https/%s connectivity check succeeded: %d/%d (%.0f%%)",
		network,
		succeeded,
		checks,
		successFraction*100,
	)
	return true
}

func checkHTTPSConnection(ctx context.Context, network string) (ok bool) {
	testDomain := getRandomTestDomain()
	code, err := CheckHTTPSConnection(ctx, network, testDomain)
	if err != nil {
		log.Tracer(ctx).Debugf("spn/patrol: https/%s connect check failed: %s: %s", network, testDomain, err)
		return false
	}

	log.Tracer(ctx).Tracef("spn/patrol: https/%s connect check succeeded: %s [%d]", network, testDomain, code)
	return true
}

// CheckHTTPSConnection checks if a HTTPS connection to the given domain can be established.
func CheckHTTPSConnection(ctx context.Context, network, domain string) (statusCode int, err error) {
	// Check network parameter.
	switch network {
	case "tcp4":
	case "tcp6":
	default:
		return 0, fmt.Errorf("provided unsupported network: %s", network)
	}

	// Build URL.
	// Use HTTPS to ensure that we have really communicated with the desired
	// server and not with an intermediate.
	url := fmt.Sprintf("https://%s/", domain)

	// Prepare all parts of the request.
	// TODO: Evaluate if we want to change the User-Agent.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	dialer := &net.Dialer{
		Timeout:       15 * time.Second,
		LocalAddr:     conf.GetBindAddr(network),
		FallbackDelay: -1, // Disables Fast Fallback from IPv6 to IPv4.
		KeepAlive:     -1, // Disable keep-alive.
	}
	dialWithNet := func(ctx context.Context, _, addr string) (net.Conn, error) {
		// Ignore network by http client.
		// Instead, force either tcp4 or tcp6.
		return dialer.DialContext(ctx, network, addr)
	}
	client := &http.Client{
		Transport: &http.Transport{
			DialContext:         dialWithNet,
			DisableKeepAlives:   true,
			DisableCompression:  true,
			TLSHandshakeTimeout: 15 * time.Second,
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Timeout: 30 * time.Second,
	}

	// Make request to server.
	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("failed to send http request: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		return resp.StatusCode, fmt.Errorf("unexpected status code: %s", resp.Status)
	}

	return resp.StatusCode, nil
}
