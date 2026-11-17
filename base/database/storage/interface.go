package storage

import (
	"context"
	"time"

	"github.com/safing/portmaster/base/database/iterator"
	"github.com/safing/portmaster/base/database/query"
	"github.com/safing/portmaster/base/database/record"
)

// Interface defines the database storage API.
type Interface interface {
	// Primary Interface
	Get(key string) (record.Record, error)
	Put(m record.Record) (record.Record, error)
	Delete(key string) error
	Query(q *query.Query, local, internal bool) (*iterator.Iterator, error)

	// Information and Control
	ReadOnly() bool
	Injected() bool
	Shutdown() error

	// Mandatory Record Maintenance
	MaintainRecordStates(ctx context.Context, purgeDeletedBefore time.Time, shadowDelete bool) error
}

// MetaHandler defines the database storage API for backends that support optimized fetching of only the metadata.
type MetaHandler interface {
	GetMeta(key string) (*record.Meta, error)
}

// Maintainer defines the database storage API for backends that require regular maintenance.
type Maintainer interface {
	Maintain(ctx context.Context) error
	MaintainThorough(ctx context.Context) error
}

// Batcher defines the database storage API for backends that support batch operations.
type Batcher interface {
	PutMany(shadowDelete bool) (batch chan<- record.Record, errs <-chan error)
}

// AbortableBatcher defines the database storage API for backends that can
// discard a batch instead of storing it.
//
// PutManyAbortable works like Batcher.PutMany: the batch is fed through the
// batch channel and ends when that channel is closed, and exactly one value
// is sent on the error channel. In addition, the batch is aborted when ctx is
// done before the batch channel is closed: none of its records are stored,
// records stored before are left untouched and the result is an error that
// wraps ErrBatchAborted. An aborted batch is never reported as success.
//
// To abort, cancel ctx first and then close the batch channel as usual. The
// storage must check ctx when it sees the closed batch channel, so that this
// order always aborts. Cancelling ctx after the batch channel was closed
// does not reliably abort the batch anymore, and has no effect once the
// result was sent. Without a cancellation, PutManyAbortable must behave
// exactly like PutMany.
type AbortableBatcher interface {
	Batcher
	PutManyAbortable(ctx context.Context, shadowDelete bool) (batch chan<- record.Record, errs <-chan error)
}

// Purger defines the database storage API for backends that support the purge operation.
type Purger interface {
	Purge(ctx context.Context, q *query.Query, local, internal, shadowDelete bool) (int, error)
}

// PurgeOlderThan defines the database storage API for backends that support the PurgeOlderThan operation.
type PurgeOlderThan interface {
	PurgeOlderThan(ctx context.Context, prefix string, purgeBefore time.Time, local, internal, shadowDelete bool) (int, error)
}
