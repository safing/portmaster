package navigator

import (
	"fmt"
	"strings"

	"github.com/safing/portmaster/base/database"
	"github.com/safing/portmaster/base/database/iterator"
	"github.com/safing/portmaster/base/database/query"
	"github.com/safing/portmaster/base/database/record"
	"github.com/safing/portmaster/base/database/storage"
	"github.com/safing/portmaster/service/mgr"
)

var mapDBController *database.Controller

// StorageInterface provices a storage.Interface to the
// configuration manager.
type StorageInterface struct {
	storage.InjectBase
}

// Database prefixes:
// Pins:       map:main/<Hub ID>
// DNS Requests:    network:tree/<PID>/dns/<ID>
// IP Connections:  network:tree/<PID>/ip/<ID>

func makeDBKey(mapName, hubID string) string {
	return fmt.Sprintf("map:%s/%s", mapName, hubID)
}

func parseDBKey(key string) (mapName, hubID string) {
	// Split into segments.
	segments := strings.Split(key, "/")

	// Keys have 1 or 2 segments.
	switch len(segments) {
	case 1:
		return segments[0], ""
	case 2:
		return segments[0], segments[1]
	default:
		return "", ""
	}
}

// Get returns a database record.
func (s *StorageInterface) Get(key string) (record.Record, error) {
	// Parse key and check if valid.
	mapName, hubID := parseDBKey(key)
	if mapName == "" || hubID == "" {
		return nil, storage.ErrNotFound
	}

	// Get map.
	m, ok := getMapForAPI(mapName)
	if !ok {
		return nil, storage.ErrNotFound
	}

	// Get Pin from map.
	pin, ok := m.GetPin(hubID)
	if !ok {
		return nil, storage.ErrNotFound
	}
	return pin.Export(), nil
}

// Query returns a an iterator for the supplied query.
func (s *StorageInterface) Query(q *query.Query, local, internal bool) (*iterator.Iterator, error) {
	// Parse key and check if valid.
	mapName, _ := parseDBKey(q.DatabaseKeyPrefix())
	if mapName == "" {
		return nil, storage.ErrNotFound
	}

	// Get map.
	m, ok := getMapForAPI(mapName)
	if !ok {
		return nil, storage.ErrNotFound
	}

	// Start query worker.
	it := iterator.New()
	module.mgr.Go("map query", func(_ *mgr.WorkerCtx) error {
		s.processQuery(m, q, it)
		return nil
	})

	return it, nil
}

func (s *StorageInterface) processQuery(m *Map, q *query.Query, it *iterator.Iterator) {
	// Return all matching pins.
	for _, pin := range m.sortedPins(true) {
		export := pin.Export()
		if q.Matches(export) {
			select {
			case it.Next <- export:
			case <-it.Done:
				return
			}
		}
	}

	it.Finish(nil)
}

func registerMapDatabase() error {
	_, err := database.Register(&database.Database{
		Name:        "map",
		Description: "SPN Network Maps",
		StorageType: database.StorageTypeInjected,
	})
	if err != nil {
		return err
	}

	controller, err := database.InjectDatabase("map", &StorageInterface{})
	if err != nil {
		return err
	}

	mapDBController = controller
	return nil
}

func withdrawMapDatabase() {
	mapDBController.Withdraw()
}

// PushPinChanges pushes all changed pins to subscribers.
func (m *Map) PushPinChanges() {
	module.mgr.Go("push pin changes", m.pushPinChangesWorker)
}

func (m *Map) pushPinChangesWorker(ctx *mgr.WorkerCtx) error {
	m.RLock()
	defer m.RUnlock()

	for _, pin := range m.all {
		if pin.pushChanges.SetToIf(true, false) {
			mapDBController.PushUpdate(pin.Export())
		}
	}

	return nil
}

// pushChange pushes changes of the pin, if the pushChanges flag is set.
func (pin *Pin) pushChange() {
	// Check before starting the worker.
	if pin.pushChanges.IsNotSet() {
		return
	}

	// Start worker to push changes.
	module.mgr.Go("push pin change", func(ctx *mgr.WorkerCtx) error {
		if pin.pushChanges.SetToIf(true, false) {
			mapDBController.PushUpdate(pin.Export())
		}
		return nil
	})
}
