package storage

import (
	"context"
	"errors"
	"time"

	"github.com/safing/portmaster/base/database/iterator"
	"github.com/safing/portmaster/base/database/query"
	"github.com/safing/portmaster/base/database/record"
)

// ErrNotImplemented is returned when a function is not implemented by a storage.
var ErrNotImplemented = errors.New("not implemented")

// InjectBase is a dummy base structure to reduce boilerplate code for injected storage interfaces.
type InjectBase struct{}

// Compile time interface check.
var _ Interface = &InjectBase{}

// Get returns a database record.
func (i *InjectBase) Get(key string) (record.Record, error) {
	return nil, ErrNotImplemented
}

// Put stores a record in the database.
func (i *InjectBase) Put(m record.Record) (record.Record, error) {
	return nil, ErrNotImplemented
}

// Delete deletes a record from the database.
func (i *InjectBase) Delete(key string) error {
	return ErrNotImplemented
}

// Query returns a an iterator for the supplied query.
func (i *InjectBase) Query(q *query.Query, local, internal bool) (*iterator.Iterator, error) {
	return nil, ErrNotImplemented
}

// ReadOnly returns whether the database is read only.
func (i *InjectBase) ReadOnly() bool {
	return true
}

// Injected returns whether the database is injected.
func (i *InjectBase) Injected() bool {
	return true
}

// MaintainRecordStates maintains records states in the database.
func (i *InjectBase) MaintainRecordStates(ctx context.Context, purgeDeletedBefore time.Time, shadowDelete bool) error {
	return nil
}

// Shutdown shuts down the database.
func (i *InjectBase) Shutdown() error {
	return nil
}
