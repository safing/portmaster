package metrics

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/safing/portmaster/base/api"
	"github.com/safing/portmaster/base/config"
	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/service/mgr"
)

func registerAPI() error {
	api.RegisterHandler("/metrics", &metricsAPI{})

	if err := api.RegisterEndpoint(api.Endpoint{
		Name:        "Export Registered Metrics",
		Description: "List all registered metrics with their metadata.",
		Path:        "metrics/list",
		Read:        api.Dynamic,
		StructFunc: func(ar *api.Request) (any, error) {
			return ExportMetrics(ar.AuthToken.Read), nil
		},
	}); err != nil {
		return err
	}

	if err := api.RegisterEndpoint(api.Endpoint{
		Name:        "Export Metric Values",
		Description: "List all exportable metric values.",
		Path:        "metrics/values",
		Read:        api.Dynamic,
		Parameters: []api.Parameter{{
			Method:      http.MethodGet,
			Field:       "internal-only",
			Description: "Specify to only return metrics with an alternative internal ID.",
		}},
		StructFunc: func(ar *api.Request) (any, error) {
			return ExportValues(
				ar.AuthToken.Read,
				ar.Request.URL.Query().Has("internal-only"),
			), nil
		},
	}); err != nil {
		return err
	}

	return nil
}

type metricsAPI struct{}

func (m *metricsAPI) ReadPermission(*http.Request) api.Permission { return api.Dynamic }

func (m *metricsAPI) WritePermission(*http.Request) api.Permission { return api.NotSupported }

func (m *metricsAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Get API Request for permission and query.
	ar := api.GetAPIRequest(r)
	if ar == nil {
		http.Error(w, "Missing API Request.", http.StatusInternalServerError)
		return
	}

	// Get expertise level from query.
	expertiseLevel := config.ExpertiseLevelDeveloper
	switch ar.Request.URL.Query().Get("level") {
	case config.ExpertiseLevelNameUser:
		expertiseLevel = config.ExpertiseLevelUser
	case config.ExpertiseLevelNameExpert:
		expertiseLevel = config.ExpertiseLevelExpert
	case config.ExpertiseLevelNameDeveloper:
		expertiseLevel = config.ExpertiseLevelDeveloper
	}

	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	WriteMetrics(w, ar.AuthToken.Read, expertiseLevel)
}

// WriteMetrics writes all metrics that match the given permission and
// expertiseLevel to the given writer.
func WriteMetrics(w io.Writer, permission api.Permission, expertiseLevel config.ExpertiseLevel) {
	registryLock.RLock()
	defer registryLock.RUnlock()

	// Write all matching metrics.
	for _, metric := range registry {
		if permission >= metric.Opts().Permission &&
			expertiseLevel >= metric.Opts().ExpertiseLevel {
			metric.WritePrometheus(w)
		}
	}
}

func writeMetricsTo(ctx context.Context, url string) error {
	// First, collect metrics into buffer.
	buf := &bytes.Buffer{}
	WriteMetrics(buf, api.PermitSelf, config.ExpertiseLevelDeveloper)

	// Check if there is something to send.
	if buf.Len() == 0 {
		log.Debugf("metrics: not pushing metrics, nothing to send")
		return nil
	}

	// Create request
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, buf)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Send.
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	// Check return status.
	if resp.StatusCode >= 200 && resp.StatusCode <= 299 {
		return nil
	}

	// Get and return error.
	body, _ := io.ReadAll(resp.Body)
	return fmt.Errorf(
		"got %s while writing metrics to %s: %s",
		resp.Status,
		url,
		body,
	)
}

func metricsWriter(ctx *mgr.WorkerCtx) error {
	pushURL := pushOption()
	module.metricTicker = mgr.NewSleepyTicker(1*time.Minute, 0)
	defer module.metricTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-module.metricTicker.Wait():
			err := writeMetricsTo(ctx.Ctx(), pushURL)
			if err != nil {
				return err
			}
		}
	}
}
