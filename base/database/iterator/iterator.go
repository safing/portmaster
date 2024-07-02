package iterator

import (
	"sync"

	"github.com/tevino/abool"

	"github.com/safing/portmaster/base/database/record"
)

// Iterator defines the iterator structure.
type Iterator struct {
	Next chan record.Record
	Done chan struct{}

	errLock    sync.Mutex
	err        error
	doneClosed *abool.AtomicBool
}

// New creates a new Iterator.
func New() *Iterator {
	return &Iterator{
		Next:       make(chan record.Record, 10),
		Done:       make(chan struct{}),
		doneClosed: abool.NewBool(false),
	}
}

// Finish is called be the storage to signal the end of the query results.
func (it *Iterator) Finish(err error) {
	close(it.Next)
	if it.doneClosed.SetToIf(false, true) {
		close(it.Done)
	}

	it.errLock.Lock()
	defer it.errLock.Unlock()
	it.err = err
}

// Cancel is called by the iteration consumer to cancel the running query.
func (it *Iterator) Cancel() {
	if it.doneClosed.SetToIf(false, true) {
		close(it.Done)
	}
}

// Err returns the iterator error, if exists.
func (it *Iterator) Err() error {
	it.errLock.Lock()
	defer it.errLock.Unlock()
	return it.err
}
