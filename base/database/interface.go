package database

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/bluele/gcache"
	"github.com/tevino/abool"

	"github.com/safing/portmaster/base/database/accessor"
	"github.com/safing/portmaster/base/database/iterator"
	"github.com/safing/portmaster/base/database/query"
	"github.com/safing/portmaster/base/database/record"
)

const (
	getDBFromKey = ""
)

// Interface provides a method to access the database with attached options.
type Interface struct {
	options *Options
	cache   gcache.Cache

	writeCache        map[string]record.Record
	writeCacheLock    sync.Mutex
	triggerCacheWrite chan struct{}
}

// Options holds options that may be set for an Interface instance.
type Options struct {
	// Local specifies if the interface is used by an actor on the local device.
	// Setting both the Local and Internal flags will bring performance
	// improvements because less checks are needed.
	Local bool

	// Internal specifies if the interface is used by an actor within the
	// software. Setting both the Local and Internal flags will bring performance
	// improvements because less checks are needed.
	Internal bool

	// AlwaysMakeSecret will have the interface mark all saved records as secret.
	// This means that they will be only accessible by an internal interface.
	AlwaysMakeSecret bool

	// AlwaysMakeCrownjewel will have the interface mark all saved records as
	// crown jewels. This means that they will be only accessible by a local
	// interface.
	AlwaysMakeCrownjewel bool

	// AlwaysSetRelativateExpiry will have the interface set a relative expiry,
	// based on the current time, on all saved records.
	AlwaysSetRelativateExpiry int64

	// AlwaysSetAbsoluteExpiry will have the interface set an absolute expiry on
	// all saved records.
	AlwaysSetAbsoluteExpiry int64

	// CacheSize defines that a cache should be used for this interface and
	// defines it's size.
	// Caching comes with an important caveat: If database records are changed
	// from another interface, the cache will not be invalidated for these
	// records. It will therefore serve outdated data until that record is
	// evicted from the cache.
	CacheSize int

	// DelayCachedWrites defines a database name for which cache writes should
	// be cached and batched. The database backend must support the Batcher
	// interface. This option is only valid if used with a cache.
	// Additionally, this may only be used for internal and local interfaces.
	// Please note that this means that other interfaces will not be able to
	// guarantee to serve the latest record if records are written this way.
	DelayCachedWrites string
}

// Apply applies options to the record metadata.
func (o *Options) Apply(r record.Record) {
	r.UpdateMeta()
	if o.AlwaysMakeSecret {
		r.Meta().MakeSecret()
	}
	if o.AlwaysMakeCrownjewel {
		r.Meta().MakeCrownJewel()
	}
	if o.AlwaysSetAbsoluteExpiry > 0 {
		r.Meta().SetAbsoluteExpiry(o.AlwaysSetAbsoluteExpiry)
	} else if o.AlwaysSetRelativateExpiry > 0 {
		r.Meta().SetRelativateExpiry(o.AlwaysSetRelativateExpiry)
	}
}

// HasAllPermissions returns whether the options specify the highest possible
// permissions for operations.
func (o *Options) HasAllPermissions() bool {
	return o.Local && o.Internal
}

// hasAccessPermission checks if the interface options permit access to the
// given record, locking the record for accessing it's attributes.
func (o *Options) hasAccessPermission(r record.Record) bool {
	// Check if the options specify all permissions, which makes checking the
	// record unnecessary.
	if o.HasAllPermissions() {
		return true
	}

	r.Lock()
	defer r.Unlock()

	// Check permissions against record.
	return r.Meta().CheckPermission(o.Local, o.Internal)
}

// NewInterface returns a new Interface to the database.
func NewInterface(opts *Options) *Interface {
	if opts == nil {
		opts = &Options{}
	}

	newIface := &Interface{
		options: opts,
	}
	if opts.CacheSize > 0 {
		cacheBuilder := gcache.New(opts.CacheSize).ARC()
		if opts.DelayCachedWrites != "" {
			cacheBuilder.EvictedFunc(newIface.cacheEvictHandler)
			newIface.writeCache = make(map[string]record.Record, opts.CacheSize/2)
			newIface.triggerCacheWrite = make(chan struct{})
		}
		newIface.cache = cacheBuilder.Build()
	}
	return newIface
}

// Exists return whether a record with the given key exists.
func (i *Interface) Exists(key string) (bool, error) {
	_, err := i.Get(key)
	if err != nil {
		switch {
		case errors.Is(err, ErrNotFound):
			return false, nil
		case errors.Is(err, ErrPermissionDenied):
			return true, nil
		default:
			return false, err
		}
	}
	return true, nil
}

// Get return the record with the given key.
func (i *Interface) Get(key string) (record.Record, error) {
	r, _, err := i.getRecord(getDBFromKey, key, false)
	return r, err
}

func (i *Interface) getRecord(dbName string, dbKey string, mustBeWriteable bool) (r record.Record, db *Controller, err error) { //nolint:unparam
	if dbName == "" {
		dbName, dbKey = record.ParseKey(dbKey)
	}

	db, err = getController(dbName)
	if err != nil {
		return nil, nil, err
	}

	if mustBeWriteable && db.ReadOnly() {
		return nil, db, ErrReadOnly
	}

	r = i.checkCache(dbName + ":" + dbKey)
	if r != nil {
		if !i.options.hasAccessPermission(r) {
			return nil, db, ErrPermissionDenied
		}
		return r, db, nil
	}

	r, err = db.Get(dbKey)
	if err != nil {
		return nil, db, err
	}

	if !i.options.hasAccessPermission(r) {
		return nil, db, ErrPermissionDenied
	}

	r.Lock()
	ttl := r.Meta().GetRelativeExpiry()
	r.Unlock()
	i.updateCache(
		r,
		false, // writing
		false, // remove
		ttl,   // expiry
	)

	return r, db, nil
}

func (i *Interface) getMeta(dbName string, dbKey string, mustBeWriteable bool) (m *record.Meta, db *Controller, err error) { //nolint:unparam
	if dbName == "" {
		dbName, dbKey = record.ParseKey(dbKey)
	}

	db, err = getController(dbName)
	if err != nil {
		return nil, nil, err
	}

	if mustBeWriteable && db.ReadOnly() {
		return nil, db, ErrReadOnly
	}

	r := i.checkCache(dbName + ":" + dbKey)
	if r != nil {
		if !i.options.hasAccessPermission(r) {
			return nil, db, ErrPermissionDenied
		}
		return r.Meta(), db, nil
	}

	m, err = db.GetMeta(dbKey)
	if err != nil {
		return nil, db, err
	}

	if !m.CheckPermission(i.options.Local, i.options.Internal) {
		return nil, db, ErrPermissionDenied
	}

	return m, db, nil
}

// InsertValue inserts a value into a record.
func (i *Interface) InsertValue(key string, attribute string, value interface{}) error {
	r, db, err := i.getRecord(getDBFromKey, key, true)
	if err != nil {
		return err
	}

	r.Lock()
	defer r.Unlock()

	var acc accessor.Accessor
	if r.IsWrapped() {
		wrapper, ok := r.(*record.Wrapper)
		if !ok {
			return errors.New("record is malformed (reports to be wrapped but is not of type *record.Wrapper)")
		}
		acc = accessor.NewJSONBytesAccessor(&wrapper.Data)
	} else {
		acc = accessor.NewStructAccessor(r)
	}

	err = acc.Set(attribute, value)
	if err != nil {
		return fmt.Errorf("failed to set value with %s: %w", acc.Type(), err)
	}

	i.options.Apply(r)
	return db.Put(r)
}

// Put saves a record to the database.
func (i *Interface) Put(r record.Record) (err error) {
	// get record or only database
	var db *Controller
	if !i.options.HasAllPermissions() {
		_, db, err = i.getMeta(r.DatabaseName(), r.DatabaseKey(), true)
		if err != nil && !errors.Is(err, ErrNotFound) {
			return err
		}
	} else {
		db, err = getController(r.DatabaseName())
		if err != nil {
			return err
		}
	}

	// Check if database is read only.
	if db.ReadOnly() {
		return ErrReadOnly
	}

	r.Lock()
	i.options.Apply(r)
	remove := r.Meta().IsDeleted()
	ttl := r.Meta().GetRelativeExpiry()
	r.Unlock()

	// The record may not be locked when updating the cache.
	written := i.updateCache(r, true, remove, ttl)
	if written {
		return nil
	}

	r.Lock()
	defer r.Unlock()
	return db.Put(r)
}

// PutNew saves a record to the database as a new record (ie. with new timestamps).
func (i *Interface) PutNew(r record.Record) (err error) {
	// get record or only database
	var db *Controller
	if !i.options.HasAllPermissions() {
		_, db, err = i.getMeta(r.DatabaseName(), r.DatabaseKey(), true)
		if err != nil && !errors.Is(err, ErrNotFound) {
			return err
		}
	} else {
		db, err = getController(r.DatabaseName())
		if err != nil {
			return err
		}
	}

	// Check if database is read only.
	if db.ReadOnly() {
		return ErrReadOnly
	}

	r.Lock()
	if r.Meta() != nil {
		r.Meta().Reset()
	}
	i.options.Apply(r)
	remove := r.Meta().IsDeleted()
	ttl := r.Meta().GetRelativeExpiry()
	r.Unlock()

	// The record may not be locked when updating the cache.
	written := i.updateCache(r, true, remove, ttl)
	if written {
		return nil
	}

	r.Lock()
	defer r.Unlock()
	return db.Put(r)
}

// PutMany stores many records in the database.
// Warning: This is nearly a direct database access and omits many things:
// - Record locking
// - Hooks
// - Subscriptions
// - Caching
// Use with care.
func (i *Interface) PutMany(dbName string) (put func(record.Record) error) {
	interfaceBatch := make(chan record.Record, 100)

	// permission check
	if !i.options.HasAllPermissions() {
		return func(r record.Record) error {
			return ErrPermissionDenied
		}
	}

	// get database
	db, err := getController(dbName)
	if err != nil {
		return func(r record.Record) error {
			return err
		}
	}

	// Check if database is read only.
	if db.ReadOnly() {
		return func(r record.Record) error {
			return ErrReadOnly
		}
	}

	// start database access
	dbBatch, errs := db.PutMany()
	finished := abool.New()
	var internalErr error

	// interface options proxy
	go func() {
		defer close(dbBatch) // signify that we are finished
		for {
			select {
			case r := <-interfaceBatch:
				// finished?
				if r == nil {
					return
				}
				// apply options
				i.options.Apply(r)
				// pass along
				dbBatch <- r
			case <-time.After(1 * time.Second):
				// bail out
				internalErr = errors.New("timeout: putmany unused for too long")
				finished.Set()
				return
			}
		}
	}()

	return func(r record.Record) error {
		// finished?
		if finished.IsSet() {
			// check for internal error
			if internalErr != nil {
				return internalErr
			}
			// check for previous error
			select {
			case err := <-errs:
				return err
			default:
				return errors.New("batch is closed")
			}
		}

		// finish?
		if r == nil {
			finished.Set()
			interfaceBatch <- nil // signify that we are finished
			// do not close, as this fn could be called again with nil.
			return <-errs
		}

		// check record scope
		if r.DatabaseName() != dbName {
			return errors.New("record out of database scope")
		}

		// submit
		select {
		case interfaceBatch <- r:
			return nil
		case err := <-errs:
			return err
		}
	}
}

// SetAbsoluteExpiry sets an absolute record expiry.
func (i *Interface) SetAbsoluteExpiry(key string, time int64) error {
	r, db, err := i.getRecord(getDBFromKey, key, true)
	if err != nil {
		return err
	}

	r.Lock()
	defer r.Unlock()

	i.options.Apply(r)
	r.Meta().SetAbsoluteExpiry(time)
	return db.Put(r)
}

// SetRelativateExpiry sets a relative (self-updating) record expiry.
func (i *Interface) SetRelativateExpiry(key string, duration int64) error {
	r, db, err := i.getRecord(getDBFromKey, key, true)
	if err != nil {
		return err
	}

	r.Lock()
	defer r.Unlock()

	i.options.Apply(r)
	r.Meta().SetRelativateExpiry(duration)
	return db.Put(r)
}

// MakeSecret marks the record as a secret, meaning interfacing processes, such as an UI, are denied access to the record.
func (i *Interface) MakeSecret(key string) error {
	r, db, err := i.getRecord(getDBFromKey, key, true)
	if err != nil {
		return err
	}

	r.Lock()
	defer r.Unlock()

	i.options.Apply(r)
	r.Meta().MakeSecret()
	return db.Put(r)
}

// MakeCrownJewel marks a record as a crown jewel, meaning it will only be accessible locally.
func (i *Interface) MakeCrownJewel(key string) error {
	r, db, err := i.getRecord(getDBFromKey, key, true)
	if err != nil {
		return err
	}

	r.Lock()
	defer r.Unlock()

	i.options.Apply(r)
	r.Meta().MakeCrownJewel()
	return db.Put(r)
}

// Delete deletes a record from the database.
func (i *Interface) Delete(key string) error {
	r, db, err := i.getRecord(getDBFromKey, key, true)
	if err != nil {
		return err
	}

	// Check if database is read only.
	if db.ReadOnly() {
		return ErrReadOnly
	}

	i.options.Apply(r)
	r.Meta().Delete()
	return db.Put(r)
}

// Query executes the given query on the database.
// Will not see data that is in the write cache, waiting to be written.
// Use with care with caching.
func (i *Interface) Query(q *query.Query) (*iterator.Iterator, error) {
	_, err := q.Check()
	if err != nil {
		return nil, err
	}

	db, err := getController(q.DatabaseName())
	if err != nil {
		return nil, err
	}

	// TODO: Finish caching system integration.
	// Flush the cache before we query the database.
	// i.FlushCache()

	return db.Query(q, i.options.Local, i.options.Internal)
}

// Purge deletes all records that match the given query. It returns the number
// of successful deletes and an error.
func (i *Interface) Purge(ctx context.Context, q *query.Query) (int, error) {
	_, err := q.Check()
	if err != nil {
		return 0, err
	}

	db, err := getController(q.DatabaseName())
	if err != nil {
		return 0, err
	}

	// Check if database is read only before we add to the cache.
	if db.ReadOnly() {
		return 0, ErrReadOnly
	}

	return db.Purge(ctx, q, i.options.Local, i.options.Internal)
}

// Subscribe subscribes to updates matching the given query.
func (i *Interface) Subscribe(q *query.Query) (*Subscription, error) {
	_, err := q.Check()
	if err != nil {
		return nil, err
	}

	c, err := getController(q.DatabaseName())
	if err != nil {
		return nil, err
	}

	sub := &Subscription{
		q:        q,
		local:    i.options.Local,
		internal: i.options.Internal,
		Feed:     make(chan record.Record, 1000),
	}
	c.addSubscription(sub)
	return sub, nil
}
