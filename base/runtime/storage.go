package runtime

import (
	"github.com/safing/portmaster/base/database/iterator"
	"github.com/safing/portmaster/base/database/query"
	"github.com/safing/portmaster/base/database/record"
	"github.com/safing/portmaster/base/database/storage"
)

// storageWrapper is a simple wrapper around storage.InjectBase and
// Registry and make sure the supported methods are handled by
// the registry rather than the InjectBase defaults.
// storageWrapper is mainly there to keep the method landscape of
// Registry as small as possible.
type storageWrapper struct {
	storage.InjectBase
	Registry *Registry
}

func (sw *storageWrapper) Get(key string) (record.Record, error) {
	return sw.Registry.Get(key)
}

func (sw *storageWrapper) Put(r record.Record) (record.Record, error) {
	return sw.Registry.Put(r)
}

func (sw *storageWrapper) Query(q *query.Query, local, internal bool) (*iterator.Iterator, error) {
	return sw.Registry.Query(q, local, internal)
}

func (sw *storageWrapper) ReadOnly() bool { return false }
