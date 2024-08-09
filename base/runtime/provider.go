package runtime

import (
	"errors"

	"github.com/safing/portmaster/base/database/record"
)

var (
	// ErrReadOnly should be returned from ValueProvider.Set if a
	// runtime record is considered read-only.
	ErrReadOnly = errors.New("runtime record is read-only")
	// ErrWriteOnly should be returned from ValueProvider.Get if
	// a runtime record is considered write-only.
	ErrWriteOnly = errors.New("runtime record is write-only")
)

type (
	// PushFunc is returned when registering a new value provider
	// and can be used to inform the database system about the
	// availability of a new runtime record value. Similar to
	// database.Controller.PushUpdate, the caller must hold
	// the lock for each record passed to PushFunc.
	PushFunc func(...record.Record)

	// ValueProvider provides access to a runtime-computed
	// database record.
	ValueProvider interface {
		// Set is called when the value is set from outside.
		// If the runtime value is considered read-only ErrReadOnly
		// should be returned. It is guaranteed that the key of
		// the record passed to Set is prefixed with the key used
		// to register the value provider.
		Set(r record.Record) (record.Record, error)
		// Get should return one or more records that match keyOrPrefix.
		// keyOrPrefix is guaranteed to be at least the prefix used to
		// register the ValueProvider.
		Get(keyOrPrefix string) ([]record.Record, error)
	}

	// SimpleValueSetterFunc is a convenience type for implementing a
	// write-only value provider.
	SimpleValueSetterFunc func(record.Record) (record.Record, error)

	// SimpleValueGetterFunc is a convenience type for implementing a
	// read-only value provider.
	SimpleValueGetterFunc func(keyOrPrefix string) ([]record.Record, error)
)

// Set implements ValueProvider.Set and calls fn.
func (fn SimpleValueSetterFunc) Set(r record.Record) (record.Record, error) {
	return fn(r)
}

// Get implements ValueProvider.Get and returns ErrWriteOnly.
func (SimpleValueSetterFunc) Get(_ string) ([]record.Record, error) {
	return nil, ErrWriteOnly
}

// Set implements ValueProvider.Set and returns ErrReadOnly.
func (SimpleValueGetterFunc) Set(r record.Record) (record.Record, error) {
	return nil, ErrReadOnly
}

// Get implements ValueProvider.Get and calls fn.
func (fn SimpleValueGetterFunc) Get(keyOrPrefix string) ([]record.Record, error) {
	return fn(keyOrPrefix)
}

// Compile time checks.
var (
	_ ValueProvider = SimpleValueGetterFunc(nil)
	_ ValueProvider = SimpleValueSetterFunc(nil)
)
