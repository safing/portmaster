package crew

import (
	"sync/atomic"

	"github.com/tevino/abool"

	"github.com/safing/portmaster/base/api"
	"github.com/safing/portmaster/base/metrics"
)

var (
	connectOpCnt            *metrics.Counter
	connectOpCntError       *metrics.Counter
	connectOpCntBadRequest  *metrics.Counter
	connectOpCntCanceled    *metrics.Counter
	connectOpCntFailed      *metrics.Counter
	connectOpCntConnected   *metrics.Counter
	connectOpCntRateLimited *metrics.Counter

	connectOpIncomingBytes *metrics.Counter
	connectOpOutgoingBytes *metrics.Counter

	connectOpTTCRDurationHistogram *metrics.Histogram
	connectOpTTFBDurationHistogram *metrics.Histogram
	connectOpDurationHistogram     *metrics.Histogram
	connectOpIncomingDataHistogram *metrics.Histogram
	connectOpOutgoingDataHistogram *metrics.Histogram

	metricsRegistered = abool.New()
)

func registerMetrics() (err error) {
	// Only register metrics once.
	if !metricsRegistered.SetToIf(false, true) {
		return nil
	}

	// Connect Op Stats on client.

	connectOpCnt, err = metrics.NewCounter(
		"spn/op/connect/total",
		nil,
		&metrics.Options{
			Name:       "SPN Total Connect Operations",
			InternalID: "spn_connect_count",
			Permission: api.PermitUser,
			Persist:    true,
		},
	)
	if err != nil {
		return err
	}

	// Connect Op Stats on server.

	connectOpCntOptions := &metrics.Options{
		Name:       "SPN Total Connect Operations",
		Permission: api.PermitUser,
		Persist:    true,
	}

	connectOpCntError, err = metrics.NewCounter(
		"spn/op/connect/total",
		map[string]string{"result": "error"},
		connectOpCntOptions,
	)
	if err != nil {
		return err
	}

	connectOpCntBadRequest, err = metrics.NewCounter(
		"spn/op/connect/total",
		map[string]string{"result": "bad_request"},
		connectOpCntOptions,
	)
	if err != nil {
		return err
	}

	connectOpCntCanceled, err = metrics.NewCounter(
		"spn/op/connect/total",
		map[string]string{"result": "canceled"},
		connectOpCntOptions,
	)
	if err != nil {
		return err
	}

	connectOpCntFailed, err = metrics.NewCounter(
		"spn/op/connect/total",
		map[string]string{"result": "failed"},
		connectOpCntOptions,
	)
	if err != nil {
		return err
	}

	connectOpCntConnected, err = metrics.NewCounter(
		"spn/op/connect/total",
		map[string]string{"result": "connected"},
		connectOpCntOptions,
	)
	if err != nil {
		return err
	}

	connectOpCntRateLimited, err = metrics.NewCounter(
		"spn/op/connect/total",
		map[string]string{"result": "rate_limited"},
		connectOpCntOptions,
	)
	if err != nil {
		return err
	}

	_, err = metrics.NewGauge(
		"spn/op/connect/active",
		nil,
		getActiveConnectOpsStat,
		&metrics.Options{
			Name:       "SPN Active Connect Operations",
			Permission: api.PermitUser,
		},
	)
	if err != nil {
		return err
	}

	connectOpIncomingBytes, err = metrics.NewCounter(
		"spn/op/connect/incoming/bytes",
		nil,
		&metrics.Options{
			Name:       "SPN Connect Operation Incoming Bytes",
			InternalID: "spn_connect_in_bytes",
			Permission: api.PermitUser,
			Persist:    true,
		},
	)
	if err != nil {
		return err
	}

	connectOpOutgoingBytes, err = metrics.NewCounter(
		"spn/op/connect/outgoing/bytes",
		nil,
		&metrics.Options{
			Name:       "SPN Connect Operation Outgoing Bytes",
			InternalID: "spn_connect_out_bytes",
			Permission: api.PermitUser,
			Persist:    true,
		},
	)
	if err != nil {
		return err
	}

	connectOpTTCRDurationHistogram, err = metrics.NewHistogram(
		"spn/op/connect/histogram/ttcr/seconds",
		nil,
		&metrics.Options{
			Name:       "SPN Connect Operation time-to-connect-request Histogram",
			Permission: api.PermitUser,
		},
	)
	if err != nil {
		return err
	}

	connectOpTTFBDurationHistogram, err = metrics.NewHistogram(
		"spn/op/connect/histogram/ttfb/seconds",
		nil,
		&metrics.Options{
			Name:       "SPN Connect Operation time-to-first-byte (from TTCR) Histogram",
			Permission: api.PermitUser,
		},
	)
	if err != nil {
		return err
	}

	connectOpDurationHistogram, err = metrics.NewHistogram(
		"spn/op/connect/histogram/duration/seconds",
		nil,
		&metrics.Options{
			Name:       "SPN Connect Operation Duration Histogram",
			Permission: api.PermitUser,
		},
	)
	if err != nil {
		return err
	}

	connectOpIncomingDataHistogram, err = metrics.NewHistogram(
		"spn/op/connect/histogram/incoming/bytes",
		nil,
		&metrics.Options{
			Name:       "SPN Connect Operation Downloaded Data Histogram",
			Permission: api.PermitUser,
		},
	)
	if err != nil {
		return err
	}

	connectOpOutgoingDataHistogram, err = metrics.NewHistogram(
		"spn/op/connect/histogram/outgoing/bytes",
		nil,
		&metrics.Options{
			Name:       "SPN Connect Operation Outgoing Data Histogram",
			Permission: api.PermitUser,
		},
	)
	if err != nil {
		return err
	}

	return nil
}

func getActiveConnectOpsStat() float64 {
	return float64(atomic.LoadInt64(activeConnectOps))
}
