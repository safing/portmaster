package db

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/safing/portmaster/base/db/query"
)

type QueryRunner struct {
	q       *query.Query
	iter    *Iterator
	finish  func(error)
	timeout time.Duration
}

func NewQueryRunner(q *query.Query, queueSize int, timeout time.Duration) (qr *QueryRunner, err error) {
	if err := q.Check(); err != nil {
		return nil, err
	}

	iter, finish := NewIterator(queueSize)
	r := &QueryRunner{
		q:       q,
		iter:    iter,
		finish:  finish,
		timeout: timeout,
	}

	return r, nil
}

func (r *QueryRunner) Iterator() *Iterator {
	return r.iter
}

func (r *QueryRunner) Finish(err error) {
	r.finish(err)
}

func (r *QueryRunner) Submit(ctx context.Context, record Record) (done bool) {
	// Skip non-matching records.
	if !r.q.Matches(record) {
		// Check if it time to abort.
		select {
		case <-r.iter.Done:
			return true
		case <-ctx.Done():
			r.finish(ErrCanceled)
			return true
		default:
			return false
		}
	}

	select {
	case r.iter.Next <- record:
		// Continue finding next record.
		return false
	case <-r.iter.Done:
		return true
	case <-ctx.Done():
		r.finish(ErrCanceled)
		return true

	default:
		select {
		case r.iter.Next <- record:
			// Continue finding next record.
			return false
		case <-r.iter.Done:
			return true
		case <-ctx.Done():
			r.finish(ErrCanceled)
			return true

		case <-time.After(r.timeout):
			r.finish(ErrTimeout)
			return true
		}
	}
}

// Iterator defines the iterator structure.
type Iterator struct {
	Next       chan Record
	nextClosed atomic.Bool

	Done       chan struct{}
	doneClosed atomic.Bool

	errLock sync.Mutex
	err     error
}

// New creates a new Iterator.
// Whoever feeds the iterator must end it with the returned finish function.
func NewIterator(queueSize int) (iterator *Iterator, finish func(error)) {
	it := &Iterator{
		Next: make(chan Record, queueSize),
		Done: make(chan struct{}),
	}
	return it, it.finish
}

// finish is called by the storage to signal the end of the query results.
func (it *Iterator) finish(err error) {
	it.errLock.Lock()
	it.err = err
	it.errLock.Unlock()

	if it.doneClosed.CompareAndSwap(false, true) {
		close(it.Done)
	}
	if it.nextClosed.CompareAndSwap(false, true) {
		close(it.Next)
	}
}

// Cancel is called by the iteration consumer to cancel the running query.
func (it *Iterator) Cancel() {
	if it.doneClosed.CompareAndSwap(false, true) {
		close(it.Done)
	}
}

// IsDone returns whether the iterator is done.
func (it *Iterator) IsDone() bool {
	return it.doneClosed.Load()
}

// Err returns the iterator error, if exists.
func (it *Iterator) Err() error {
	it.errLock.Lock()
	defer it.errLock.Unlock()
	return it.err
}
