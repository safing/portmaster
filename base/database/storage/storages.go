package storage

import (
	"errors"
	"fmt"
	"sync"
)

// A Factory creates a new database of it's type.
type Factory func(name, location string) (Interface, error)

var (
	storages     = make(map[string]Factory)
	storagesLock sync.Mutex
)

// Register registers a new storage type.
func Register(name string, factory Factory) error {
	storagesLock.Lock()
	defer storagesLock.Unlock()

	_, ok := storages[name]
	if ok {
		return errors.New("factory for this type already exists")
	}

	storages[name] = factory
	return nil
}

// CreateDatabase starts a new database with the given name and storageType at location.
func CreateDatabase(name, storageType, location string) (Interface, error) {
	return nil, nil
}

// StartDatabase starts a new database with the given name and storageType at location.
func StartDatabase(name, storageType, location string) (Interface, error) {
	storagesLock.Lock()
	defer storagesLock.Unlock()

	factory, ok := storages[storageType]
	if !ok {
		return nil, fmt.Errorf("storage type %s not registered", storageType)
	}

	return factory(name, location)
}
