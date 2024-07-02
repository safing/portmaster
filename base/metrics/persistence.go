package metrics

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/tevino/abool"

	"github.com/safing/portmaster/base/database"
	"github.com/safing/portmaster/base/database/record"
	"github.com/safing/portmaster/base/log"
)

var (
	storage       *metricsStorage
	storageKey    string
	storageInit   = abool.New()
	storageLoaded = abool.New()

	db = database.NewInterface(&database.Options{
		Local:    true,
		Internal: true,
	})

	// ErrAlreadyInitialized is returned when trying to initialize an option
	// more than once or if the time window for initializing is over.
	ErrAlreadyInitialized = errors.New("already initialized")
)

type metricsStorage struct {
	sync.Mutex
	record.Base

	Start    time.Time
	Counters map[string]uint64
}

// EnableMetricPersistence enables metric persistence for metrics that opted
// for it. They given key is the database key where the metric data will be
// persisted.
// This call also directly loads the stored data from the database.
// The returned error is only about loading the metrics, not about enabling
// persistence.
// May only be called once.
func EnableMetricPersistence(key string) error {
	// Check if already initialized.
	if !storageInit.SetToIf(false, true) {
		return ErrAlreadyInitialized
	}

	// Set storage key.
	storageKey = key

	// Load metrics from storage.
	var err error
	storage, err = getMetricsStorage(storageKey)
	switch {
	case err == nil:
		// Continue.
	case errors.Is(err, database.ErrNotFound):
		return nil
	default:
		return err
	}
	storageLoaded.Set()

	// Load saved state for all counter metrics.
	registryLock.RLock()
	defer registryLock.RUnlock()

	for _, m := range registry {
		counter, ok := m.(*Counter)
		if ok {
			counter.loadState()
		}
	}

	return nil
}

func (c *Counter) loadState() {
	// Check if we can and should load the state.
	if !storageLoaded.IsSet() || !c.Opts().Persist {
		return
	}

	c.Set(storage.Counters[c.LabeledID()])
}

func storePersistentMetrics() {
	// Check if persistence is enabled.
	if !storageInit.IsSet() || storageKey == "" {
		return
	}

	// Create new storage.
	newStorage := &metricsStorage{
		// TODO: This timestamp should be taken from previous save, if possible.
		Start:    time.Now(),
		Counters: make(map[string]uint64),
	}
	newStorage.SetKey(storageKey)
	// Copy values from previous version.
	if storageLoaded.IsSet() {
		newStorage.Start = storage.Start
	}

	registryLock.RLock()
	defer registryLock.RUnlock()

	// Export all counter metrics.
	for _, m := range registry {
		if m.Opts().Persist {
			counter, ok := m.(*Counter)
			if ok {
				newStorage.Counters[m.LabeledID()] = counter.Get()
			}
		}
	}

	// Save to database.
	err := db.Put(newStorage)
	if err != nil {
		log.Warningf("metrics: failed to save metrics storage to db: %s", err)
	}
}

func getMetricsStorage(key string) (*metricsStorage, error) {
	r, err := db.Get(key)
	if err != nil {
		return nil, err
	}

	// unwrap
	if r.IsWrapped() {
		// only allocate a new struct, if we need it
		newStorage := &metricsStorage{}
		err = record.Unwrap(r, newStorage)
		if err != nil {
			return nil, err
		}
		return newStorage, nil
	}

	// or adjust type
	newStorage, ok := r.(*metricsStorage)
	if !ok {
		return nil, fmt.Errorf("record not of type *metricsStorage, but %T", r)
	}
	return newStorage, nil
}
