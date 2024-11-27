package metrics

import (
	"errors"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"

	"github.com/safing/portmaster/service/mgr"
)

type Metrics struct {
	mgr      *mgr.Manager
	instance instance

	metricTicker *mgr.SleepyTicker
}

func (met *Metrics) Manager() *mgr.Manager {
	return met.mgr
}

func (met *Metrics) Start() error {
	return start()
}

func (met *Metrics) Stop() error {
	return stop()
}

func (met *Metrics) SetSleep(enabled bool) {
	if met.metricTicker != nil {
		met.metricTicker.SetSleep(enabled)
	}
}

var (
	module     *Metrics
	shimLoaded atomic.Bool

	registry     []Metric
	registryLock sync.RWMutex

	readyToRegister       bool
	firstMetricRegistered bool
	metricNamespace       string
	globalLabels          = make(map[string]string)

	// ErrAlreadyStarted is returned when an operation is only valid before the
	// first metric is registered, and is called after.
	ErrAlreadyStarted = errors.New("can only be changed before first metric is registered")

	// ErrAlreadyRegistered is returned when a metric with the same ID is
	// registered again.
	ErrAlreadyRegistered = errors.New("metric already registered")

	// ErrAlreadySet is returned when a value is already set and cannot be changed.
	ErrAlreadySet = errors.New("already set")

	// ErrInvalidOptions is returned when invalid options where provided.
	ErrInvalidOptions = errors.New("invalid options")
)

func start() error {
	// Add metric instance name as global variable if set.
	if instanceOption() != "" {
		if err := AddGlobalLabel("instance", instanceOption()); err != nil {
			return err
		}
	}

	// Mark registry as ready to register metrics.
	func() {
		registryLock.Lock()
		defer registryLock.Unlock()
		readyToRegister = true
	}()

	if err := registerInfoMetric(); err != nil {
		return err
	}

	if err := registerRuntimeMetric(); err != nil {
		return err
	}

	if err := registerHostMetrics(); err != nil {
		return err
	}

	if err := registerLogMetrics(); err != nil {
		return err
	}

	if pushOption() != "" {
		module.mgr.Go("metric pusher", metricsWriter)
	}

	return nil
}

func stop() error {
	// Wait until the metrics pusher is done, as it may have started reporting
	// and may report a higher number than we store to disk. For persistent
	// metrics it can then happen that the first report is lower than the
	// previous report, making prometheus think that all that happened since the
	// last report, due to the automatic restart detection.

	// The registry is read locked when writing metrics.
	// Write lock the registry to make sure all writes are finished.
	registryLock.Lock()
	registryLock.Unlock() //nolint:staticcheck

	storePersistentMetrics()

	return nil
}

func register(m Metric) error {
	registryLock.Lock()
	defer registryLock.Unlock()

	// Check if metric ID is already registered.
	for _, registeredMetric := range registry {
		if m.LabeledID() == registeredMetric.LabeledID() {
			return ErrAlreadyRegistered
		}
		if m.Opts().InternalID != "" &&
			m.Opts().InternalID == registeredMetric.Opts().InternalID {
			return fmt.Errorf("%w with this internal ID", ErrAlreadyRegistered)
		}
	}

	// Add new metric to registry and sort it.
	registry = append(registry, m)
	sort.Sort(byLabeledID(registry))

	// Check if we can already register.
	if !readyToRegister {
		return fmt.Errorf("registering metric %q too early", m.ID())
	}

	// Set flag that first metric is now registered.
	firstMetricRegistered = true

	return nil
}

// SetNamespace sets the namespace for all metrics. It is prefixed to all
// metric IDs.
// It must be set before any metric is registered.
// Does not affect golang runtime metrics.
func SetNamespace(namespace string) error {
	// Lock registry and check if a first metric is already registered.
	registryLock.Lock()
	defer registryLock.Unlock()
	if firstMetricRegistered {
		return ErrAlreadyStarted
	}

	// Check if the namespace is already set.
	if metricNamespace != "" {
		return ErrAlreadySet
	}

	metricNamespace = namespace
	return nil
}

// AddGlobalLabel adds a global label to all metrics.
// Global labels must be added before any metric is registered.
// Does not affect golang runtime metrics.
func AddGlobalLabel(name, value string) error {
	// Lock registry and check if a first metric is already registered.
	registryLock.Lock()
	defer registryLock.Unlock()
	if firstMetricRegistered {
		return ErrAlreadyStarted
	}

	// Check format.
	if !prometheusFormat.MatchString(name) {
		return fmt.Errorf("metric label name %q must match %s", name, PrometheusFormatRequirement)
	}

	globalLabels[name] = value
	return nil
}

type byLabeledID []Metric

func (r byLabeledID) Len() int           { return len(r) }
func (r byLabeledID) Less(i, j int) bool { return r[i].LabeledID() < r[j].LabeledID() }
func (r byLabeledID) Swap(i, j int)      { r[i], r[j] = r[j], r[i] }

func New(instance instance) (*Metrics, error) {
	if !shimLoaded.CompareAndSwap(false, true) {
		return nil, errors.New("only one instance allowed")
	}
	m := mgr.New("Metrics")
	module = &Metrics{
		mgr:      m,
		instance: instance,
	}
	if err := prepConfig(); err != nil {
		return nil, err
	}

	if err := registerAPI(); err != nil {
		return nil, err
	}
	return module, nil
}

type instance interface{}
