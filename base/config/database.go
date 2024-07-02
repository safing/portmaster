package config

import (
	"errors"
	"sort"
	"strings"

	"github.com/safing/portmaster/base/database"
	"github.com/safing/portmaster/base/database/iterator"
	"github.com/safing/portmaster/base/database/query"
	"github.com/safing/portmaster/base/database/record"
	"github.com/safing/portmaster/base/database/storage"
	"github.com/safing/portmaster/base/log"
)

var dbController *database.Controller

// StorageInterface provices a storage.Interface to the configuration manager.
type StorageInterface struct {
	storage.InjectBase
}

// Get returns a database record.
func (s *StorageInterface) Get(key string) (record.Record, error) {
	opt, err := GetOption(key)
	if err != nil {
		return nil, storage.ErrNotFound
	}

	return opt.Export()
}

// Put stores a record in the database.
func (s *StorageInterface) Put(r record.Record) (record.Record, error) {
	if r.Meta().Deleted > 0 {
		return r, setConfigOption(r.DatabaseKey(), nil, false)
	}

	acc := r.GetAccessor(r)
	if acc == nil {
		return nil, errors.New("invalid data")
	}

	val, ok := acc.Get("Value")
	if !ok || val == nil {
		err := setConfigOption(r.DatabaseKey(), nil, false)
		if err != nil {
			return nil, err
		}
		return s.Get(r.DatabaseKey())
	}

	option, err := GetOption(r.DatabaseKey())
	if err != nil {
		return nil, err
	}

	var value interface{}
	switch option.OptType {
	case OptTypeString:
		value, ok = acc.GetString("Value")
	case OptTypeStringArray:
		value, ok = acc.GetStringArray("Value")
	case OptTypeInt:
		value, ok = acc.GetInt("Value")
	case OptTypeBool:
		value, ok = acc.GetBool("Value")
	case optTypeAny:
		ok = false
	}
	if !ok {
		return nil, errors.New("received invalid value in \"Value\"")
	}

	if err := setConfigOption(r.DatabaseKey(), value, false); err != nil {
		return nil, err
	}
	return option.Export()
}

// Delete deletes a record from the database.
func (s *StorageInterface) Delete(key string) error {
	return setConfigOption(key, nil, false)
}

// Query returns a an iterator for the supplied query.
func (s *StorageInterface) Query(q *query.Query, local, internal bool) (*iterator.Iterator, error) {
	optionsLock.RLock()
	defer optionsLock.RUnlock()

	it := iterator.New()
	var opts []*Option
	for _, opt := range options {
		if strings.HasPrefix(opt.Key, q.DatabaseKeyPrefix()) {
			opts = append(opts, opt)
		}
	}

	go s.processQuery(it, opts)

	return it, nil
}

func (s *StorageInterface) processQuery(it *iterator.Iterator, opts []*Option) {
	sort.Sort(sortByKey(opts))

	for _, opt := range opts {
		r, err := opt.Export()
		if err != nil {
			it.Finish(err)
			return
		}
		it.Next <- r
	}

	it.Finish(nil)
}

// ReadOnly returns whether the database is read only.
func (s *StorageInterface) ReadOnly() bool {
	return false
}

func registerAsDatabase() error {
	_, err := database.Register(&database.Database{
		Name:        "config",
		Description: "Configuration Manager",
		StorageType: "injected",
	})
	if err != nil {
		return err
	}

	controller, err := database.InjectDatabase("config", &StorageInterface{})
	if err != nil {
		return err
	}

	dbController = controller
	return nil
}

// handleOptionUpdate updates the expertise and release level options,
// if required, and eventually pushes a update for the option.
// The caller must hold the option lock.
func handleOptionUpdate(option *Option, push bool) {
	if expertiseLevelOptionFlag.IsSet() && option == expertiseLevelOption {
		updateExpertiseLevel()
	}

	if releaseLevelOptionFlag.IsSet() && option == releaseLevelOption {
		updateReleaseLevel()
	}

	if push {
		pushUpdate(option)
	}
}

// pushUpdate pushes an database update notification for option.
// The caller must hold the option lock.
func pushUpdate(option *Option) {
	r, err := option.export()
	if err != nil {
		log.Errorf("failed to export option to push update: %s", err)
	} else {
		dbController.PushUpdate(r)
	}
}
