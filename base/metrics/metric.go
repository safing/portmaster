package metrics

import (
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"

	vm "github.com/VictoriaMetrics/metrics"

	"github.com/safing/portmaster/base/api"
	"github.com/safing/portmaster/base/config"
)

// PrometheusFormatRequirement is required format defined by prometheus for
// metric and label names.
const (
	prometheusBaseFormt         = "[a-zA-Z_][a-zA-Z0-9_]*"
	PrometheusFormatRequirement = "^" + prometheusBaseFormt + "$"
)

var prometheusFormat = regexp.MustCompile(PrometheusFormatRequirement)

// Metric represents one or more metrics.
type Metric interface {
	ID() string
	LabeledID() string
	Opts() *Options
	WritePrometheus(w io.Writer)
}

type metricBase struct {
	Identifier        string
	Labels            map[string]string
	LabeledIdentifier string
	Options           *Options
	set               *vm.Set
}

// Options can be used to set advanced metric settings.
type Options struct {
	// Name defines an optional human readable name for the metric.
	Name string

	// InternalID specifies an alternative internal ID that will be used when
	// exposing the metric via the API in a structured format.
	InternalID string

	// AlertLimit defines an upper limit that triggers an alert.
	AlertLimit float64

	// AlertTimeframe defines an optional timeframe in seconds for which the
	// AlertLimit should be interpreted in.
	AlertTimeframe float64

	// Permission defines the permission that is required to read the metric.
	Permission api.Permission

	// ExpertiseLevel defines the expertise level that the metric is meant for.
	ExpertiseLevel config.ExpertiseLevel

	// Persist enabled persisting the metric on shutdown and loading the previous
	// value at start. This is only supported for counters.
	Persist bool
}

func newMetricBase(id string, labels map[string]string, opts Options) (*metricBase, error) {
	// Check formats.
	if !prometheusFormat.MatchString(strings.ReplaceAll(id, "/", "_")) {
		return nil, fmt.Errorf("metric name %q must match %s", id, PrometheusFormatRequirement)
	}
	for labelName := range labels {
		if !prometheusFormat.MatchString(labelName) {
			return nil, fmt.Errorf("metric label name %q must match %s", labelName, PrometheusFormatRequirement)
		}
	}

	// Check permission.
	if opts.Permission < api.PermitAnyone {
		// Default to PermitUser.
		opts.Permission = api.PermitUser
	}

	// Ensure that labels is a map.
	if labels == nil {
		labels = make(map[string]string)
	}

	// Create metric base.
	base := &metricBase{
		Identifier: id,
		Labels:     labels,
		Options:    &opts,
		set:        vm.NewSet(),
	}
	base.LabeledIdentifier = base.buildLabeledID()
	return base, nil
}

// ID returns the given ID of the metric.
func (m *metricBase) ID() string {
	return m.Identifier
}

// LabeledID returns the Prometheus-compatible labeled ID of the metric.
func (m *metricBase) LabeledID() string {
	return m.LabeledIdentifier
}

// Opts returns the metric options. They  may not be modified.
func (m *metricBase) Opts() *Options {
	return m.Options
}

// WritePrometheus writes the metric in the prometheus format to the given writer.
func (m *metricBase) WritePrometheus(w io.Writer) {
	m.set.WritePrometheus(w)
}

func (m *metricBase) buildLabeledID() string {
	// Because we use the namespace and the global flags here, we need to flag
	// them as immutable.
	registryLock.Lock()
	defer registryLock.Unlock()
	firstMetricRegistered = true

	// Build ID from Identifier.
	metricID := strings.TrimSpace(strings.ReplaceAll(m.Identifier, "/", "_"))

	// Add namespace to ID.
	if metricNamespace != "" {
		metricID = metricNamespace + "_" + metricID
	}

	// Return now if no labels are defined.
	if len(globalLabels) == 0 && len(m.Labels) == 0 {
		return metricID
	}

	// Add global labels to the custom ones, if they don't exist yet.
	for labelName, labelValue := range globalLabels {
		if _, ok := m.Labels[labelName]; !ok {
			m.Labels[labelName] = labelValue
		}
	}

	// Render labels into a slice and sort them in order to make the labeled ID
	// reproducible.
	labels := make([]string, 0, len(m.Labels))
	for labelName, labelValue := range m.Labels {
		labels = append(labels, fmt.Sprintf("%s=%q", labelName, labelValue))
	}
	sort.Strings(labels)

	// Return fully labaled ID.
	return fmt.Sprintf("%s{%s}", metricID, strings.Join(labels, ","))
}

// Split metrics into sets, according to the API Auth Levels, which will also correspond to the UI Mode levels. SPN // nodes will also allow public access to metrics with the permission "PermitAnyone".
// Save "life-long" metrics on shutdown and load them at start.
// Generate the correct metric name and labels.
// Expose metrics via http, but also via the runtime DB in order to push metrics to the UI.
// The UI will have to parse the prometheus metrics format and will not be able to immediately present historical data, // but data will have to be built.
// Provide the option to push metrics to a prometheus push gateway, this is especially helpful when gathering data from // loads of SPN nodes.
