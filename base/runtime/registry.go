package runtime

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/armon/go-radix"
	"golang.org/x/sync/errgroup"

	"github.com/safing/portmaster/base/database"
	"github.com/safing/portmaster/base/database/iterator"
	"github.com/safing/portmaster/base/database/query"
	"github.com/safing/portmaster/base/database/record"
	"github.com/safing/portmaster/base/database/storage"
	"github.com/safing/portmaster/base/log"
)

var (
	// ErrKeyTaken is returned when trying to register
	// a value provider at database key or prefix that
	// is already occupied by another provider.
	ErrKeyTaken = errors.New("runtime key or prefix already used")
	// ErrKeyUnmanaged is returned when a Put operation
	// on an unmanaged key is performed.
	ErrKeyUnmanaged = errors.New("runtime key not managed by any provider")
	// ErrInjected is returned by Registry.InjectAsDatabase
	// if the registry has already been injected.
	ErrInjected = errors.New("registry already injected")
)

// Registry keeps track of registered runtime
// value providers and exposes them via an
// injected database. Users normally just need
// to use the defaul registry provided by this
// package but may consider creating a dedicated
// runtime registry on their own. Registry uses
// a radix tree for value providers and their
// chosen database key/prefix.
type Registry struct {
	l            sync.RWMutex
	providers    *radix.Tree
	dbController *database.Controller
	dbName       string
}

// keyedValueProvider simply wraps a value provider with it's
// registration prefix.
type keyedValueProvider struct {
	ValueProvider
	key string
}

// NewRegistry returns a new registry.
func NewRegistry() *Registry {
	return &Registry{
		providers: radix.New(),
	}
}

func isPrefixKey(key string) bool {
	return strings.HasSuffix(key, "/")
}

// DatabaseName returns the name of the database where the
// registry has been injected. It returns an empty string
// if InjectAsDatabase has not been called.
func (r *Registry) DatabaseName() string {
	r.l.RLock()
	defer r.l.RUnlock()

	return r.dbName
}

// InjectAsDatabase injects the registry as the storage
// database for name.
func (r *Registry) InjectAsDatabase(name string) error {
	r.l.Lock()
	defer r.l.Unlock()

	if r.dbController != nil {
		return ErrInjected
	}

	ctrl, err := database.InjectDatabase(name, r.asStorage())
	if err != nil {
		return err
	}

	r.dbName = name
	r.dbController = ctrl

	return nil
}

// Register registers a new value provider p under keyOrPrefix. The
// returned PushFunc can be used to send update notitifcations to
// database subscribers. Note that keyOrPrefix must end in '/' to be
// accepted as a prefix.
func (r *Registry) Register(keyOrPrefix string, p ValueProvider) (PushFunc, error) {
	r.l.Lock()
	defer r.l.Unlock()

	// search if there's a provider registered for a prefix
	// that matches or is equal to keyOrPrefix.
	key, _, ok := r.providers.LongestPrefix(keyOrPrefix)
	if ok && (isPrefixKey(key) || key == keyOrPrefix) {
		return nil, fmt.Errorf("%w: found provider on %s", ErrKeyTaken, key)
	}

	// if keyOrPrefix is a prefix there must not be any provider
	// registered for a key that matches keyOrPrefix.
	if isPrefixKey(keyOrPrefix) {
		foundProvider := ""
		r.providers.WalkPrefix(keyOrPrefix, func(s string, _ interface{}) bool {
			foundProvider = s
			return true
		})
		if foundProvider != "" {
			return nil, fmt.Errorf("%w: found provider on %s", ErrKeyTaken, foundProvider)
		}
	}

	r.providers.Insert(keyOrPrefix, &keyedValueProvider{
		ValueProvider: TraceProvider(p),
		key:           keyOrPrefix,
	})

	log.Tracef("runtime: registered new provider at %s", keyOrPrefix)

	return func(records ...record.Record) {
		r.l.RLock()
		defer r.l.RUnlock()

		if r.dbController == nil {
			return
		}

		for _, rec := range records {
			r.dbController.PushUpdate(rec)
		}
	}, nil
}

// Get returns the runtime value that is identified by key.
// It implements the storage.Interface.
func (r *Registry) Get(key string) (record.Record, error) {
	provider := r.getMatchingProvider(key)
	if provider == nil {
		return nil, database.ErrNotFound
	}

	records, err := provider.Get(key)
	if err != nil {
		// instead of returning ErrWriteOnly to the database interface
		// we wrap it in ErrNotFound so the records effectively gets
		// hidden.
		if errors.Is(err, ErrWriteOnly) {
			return nil, database.ErrNotFound
		}
		return nil, err
	}

	// Get performs an exact match so filter out
	// and values that do not match key.
	for _, r := range records {
		if r.DatabaseKey() == key {
			return r, nil
		}
	}

	return nil, database.ErrNotFound
}

// Put stores the record m in the runtime database. Note that
// ErrReadOnly is returned if there's no value provider responsible
// for m.Key().
func (r *Registry) Put(m record.Record) (record.Record, error) {
	provider := r.getMatchingProvider(m.DatabaseKey())
	if provider == nil {
		// if there's no provider for the given value
		// return ErrKeyUnmanaged.
		return nil, ErrKeyUnmanaged
	}

	res, err := provider.Set(m)
	if err != nil {
		return nil, err
	}
	return res, nil
}

// Query performs a query on the runtime registry returning all
// records across all value providers that match q.
// Query implements the storage.Storage interface.
func (r *Registry) Query(q *query.Query, local, internal bool) (*iterator.Iterator, error) {
	if _, err := q.Check(); err != nil {
		return nil, fmt.Errorf("invalid query: %w", err)
	}

	searchPrefix := q.DatabaseKeyPrefix()
	providers := r.collectProviderByPrefix(searchPrefix)
	if len(providers) == 0 {
		return nil, fmt.Errorf("%w: for key %s", ErrKeyUnmanaged, searchPrefix)
	}

	iter := iterator.New()

	grp := new(errgroup.Group)
	for idx := range providers {
		p := providers[idx]

		grp.Go(func() (err error) {
			defer recovery(&err)

			key := p.key
			if len(searchPrefix) > len(key) {
				key = searchPrefix
			}

			records, err := p.Get(key)
			if err != nil {
				if errors.Is(err, ErrWriteOnly) {
					return nil
				}
				return err
			}

			for _, r := range records {
				r.Lock()
				var (
					matchesKey = q.MatchesKey(r.DatabaseKey())
					isValid    = r.Meta().CheckValidity()
					isAllowed  = r.Meta().CheckPermission(local, internal)

					allowed = matchesKey && isValid && isAllowed
				)
				if allowed {
					allowed = q.MatchesRecord(r)
				}
				r.Unlock()

				if !allowed {
					log.Tracef("runtime: not sending %s for query %s. matchesKey=%v isValid=%v isAllowed=%v", r.DatabaseKey(), searchPrefix, matchesKey, isValid, isAllowed)
					continue
				}

				select {
				case iter.Next <- r:
				case <-iter.Done:
					return nil
				}
			}

			return nil
		})
	}

	go func() {
		err := grp.Wait()
		iter.Finish(err)
	}()

	return iter, nil
}

func (r *Registry) getMatchingProvider(key string) *keyedValueProvider {
	r.l.RLock()
	defer r.l.RUnlock()

	providerKey, provider, ok := r.providers.LongestPrefix(key)
	if !ok {
		return nil
	}

	if !isPrefixKey(providerKey) && providerKey != key {
		return nil
	}

	return provider.(*keyedValueProvider) //nolint:forcetypeassert
}

func (r *Registry) collectProviderByPrefix(prefix string) []*keyedValueProvider {
	r.l.RLock()
	defer r.l.RUnlock()

	// if there's a LongestPrefix provider that's the only one
	// we need to ask
	if _, p, ok := r.providers.LongestPrefix(prefix); ok {
		return []*keyedValueProvider{p.(*keyedValueProvider)} //nolint:forcetypeassert
	}

	var providers []*keyedValueProvider
	r.providers.WalkPrefix(prefix, func(key string, p interface{}) bool {
		providers = append(providers, p.(*keyedValueProvider)) //nolint:forcetypeassert
		return false
	})

	return providers
}

// GetRegistrationKeys returns a list of all provider registration
// keys or prefixes.
func (r *Registry) GetRegistrationKeys() []string {
	r.l.RLock()
	defer r.l.RUnlock()

	var keys []string

	r.providers.Walk(func(key string, p interface{}) bool {
		keys = append(keys, key)
		return false
	})
	return keys
}

// asStorage returns a storage.Interface compatible struct
// that is backed by r.
func (r *Registry) asStorage() storage.Interface {
	return &storageWrapper{
		Registry: r,
	}
}

func recovery(err *error) {
	if x := recover(); x != nil {
		if e, ok := x.(error); ok {
			*err = e
			return
		}

		*err = fmt.Errorf("%v", x)
	}
}
