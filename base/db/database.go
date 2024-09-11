package db

import (
	"time"

	"github.com/safing/portmaster/base/db/query"
)

const (
	DefaultQueryTimeout        = 100 * time.Millisecond
	DefaultSubscriptionTimeout = 10 * time.Millisecond
)

// Database defines a uniform interface to be used for databases.
type Database interface {
	Exists(key string) (bool, error)
	Get(key string) (Record, error)
	Put(r Record) error
	Delete(key string) error

	BatchPut() (put func(Record) error, err error)
	BatchDelete(q *query.Query) (int, error)

	Query(q *query.Query, queueSize int) (*Iterator, error)
	Subscribe(q *query.Query, queueSize int) (*Subscription, error)
}
