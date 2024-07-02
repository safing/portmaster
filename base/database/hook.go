package database

import (
	"github.com/safing/portmaster/base/database/query"
	"github.com/safing/portmaster/base/database/record"
)

// Hook can be registered for a database query and
// will be executed at certain points during the life
// cycle of a database record.
type Hook interface {
	// UsesPreGet should return true if the hook's PreGet
	// should be called prior to loading a database record
	// from the underlying storage.
	UsesPreGet() bool
	// PreGet is called before a database record is loaded from
	// the underlying storage. A PreGet hookd may be used to
	// implement more advanced access control on database keys.
	PreGet(dbKey string) error
	// UsesPostGet should return true if the hook's PostGet
	// should be called after loading a database record from
	// the underlying storage.
	UsesPostGet() bool
	// PostGet is called after a record has been loaded form the
	// underlying storage and may perform additional mutation
	// or access check based on the records data.
	// The passed record is already locked by the database system
	// so users can safely access all data of r.
	PostGet(r record.Record) (record.Record, error)
	// UsesPrePut should return true if the hook's PrePut method
	// should be called prior to saving a record in the database.
	UsesPrePut() bool
	// PrePut is called prior to saving (creating or updating) a
	// record in the database storage. It may be used to perform
	// extended validation or mutations on the record.
	// The passed record is already locked by the database system
	// so users can safely access all data of r.
	PrePut(r record.Record) (record.Record, error)
}

// RegisteredHook is a registered database hook.
type RegisteredHook struct {
	q *query.Query
	h Hook
}

// RegisterHook registers a hook for records matching the given
// query in the database.
func RegisterHook(q *query.Query, hook Hook) (*RegisteredHook, error) {
	_, err := q.Check()
	if err != nil {
		return nil, err
	}

	c, err := getController(q.DatabaseName())
	if err != nil {
		return nil, err
	}

	rh := &RegisteredHook{
		q: q,
		h: hook,
	}

	c.hooksLock.Lock()
	defer c.hooksLock.Unlock()
	c.hooks = append(c.hooks, rh)

	return rh, nil
}

// Cancel unregisteres the hook from the database. Once
// Cancel returned the hook's methods will not be called
// anymore for updates that matched the registered query.
func (h *RegisteredHook) Cancel() error {
	c, err := getController(h.q.DatabaseName())
	if err != nil {
		return err
	}

	c.hooksLock.Lock()
	defer c.hooksLock.Unlock()

	for key, hook := range c.hooks {
		if hook.q == h.q {
			c.hooks = append(c.hooks[:key], c.hooks[key+1:]...)
			return nil
		}
	}
	return nil
}
