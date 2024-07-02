package notifications

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/safing/portmaster/base/database"
	"github.com/safing/portmaster/base/database/iterator"
	"github.com/safing/portmaster/base/database/query"
	"github.com/safing/portmaster/base/database/record"
	"github.com/safing/portmaster/base/database/storage"
	"github.com/safing/portmaster/base/log"
)

var (
	nots     = make(map[string]*Notification)
	notsLock sync.RWMutex

	dbController *database.Controller
)

// Storage interface errors.
var (
	ErrInvalidData = errors.New("invalid data, must be a notification object")
	ErrInvalidPath = errors.New("invalid path")
	ErrNoDelete    = errors.New("notifications may not be deleted, they must be handled")
)

// StorageInterface provices a storage.Interface to the configuration manager.
type StorageInterface struct {
	storage.InjectBase
}

func registerAsDatabase() error {
	_, err := database.Register(&database.Database{
		Name:        "notifications",
		Description: "Notifications",
		StorageType: "injected",
	})
	if err != nil {
		return err
	}

	controller, err := database.InjectDatabase("notifications", &StorageInterface{})
	if err != nil {
		return err
	}

	dbController = controller
	return nil
}

// Get returns a database record.
func (s *StorageInterface) Get(key string) (record.Record, error) {
	// Get EventID from key.
	if !strings.HasPrefix(key, "all/") {
		return nil, storage.ErrNotFound
	}
	key = strings.TrimPrefix(key, "all/")

	// Get notification from storage.
	n, ok := getNotification(key)
	if !ok {
		return nil, storage.ErrNotFound
	}

	return n, nil
}

func getNotification(eventID string) (n *Notification, ok bool) {
	notsLock.RLock()
	defer notsLock.RUnlock()

	n, ok = nots[eventID]
	return
}

// Query returns a an iterator for the supplied query.
func (s *StorageInterface) Query(q *query.Query, local, internal bool) (*iterator.Iterator, error) {
	it := iterator.New()
	go s.processQuery(q, it)
	// TODO: check local and internal

	return it, nil
}

func (s *StorageInterface) processQuery(q *query.Query, it *iterator.Iterator) {
	// Get a copy of the notification map.
	notsCopy := getNotsCopy()

	// send all notifications
	for _, n := range notsCopy {
		if inQuery(n, q) {
			select {
			case it.Next <- n:
			case <-it.Done:
				// make sure we don't leak this goroutine if the iterator get's cancelled
				return
			}
		}
	}

	it.Finish(nil)
}

func inQuery(n *Notification, q *query.Query) bool {
	n.lock.Lock()
	defer n.lock.Unlock()

	switch {
	case n.Meta().IsDeleted():
		return false
	case !q.MatchesKey(n.DatabaseKey()):
		return false
	case !q.MatchesRecord(n):
		return false
	}

	return true
}

// Put stores a record in the database.
func (s *StorageInterface) Put(r record.Record) (record.Record, error) {
	// record is already locked!
	key := r.DatabaseKey()
	n, err := EnsureNotification(r)
	if err != nil {
		return nil, ErrInvalidData
	}

	// transform key
	if strings.HasPrefix(key, "all/") {
		key = strings.TrimPrefix(key, "all/")
	} else {
		return nil, ErrInvalidPath
	}

	return applyUpdate(n, key)
}

func applyUpdate(n *Notification, key string) (*Notification, error) {
	// separate goroutine in order to correctly lock notsLock
	existing, ok := getNotification(key)

	// ignore if already deleted
	if !ok || existing.Meta().IsDeleted() {
		// this is a completely new notification
		// we pass pushUpdate==false because the storage
		// controller will push an update on put anyway.
		n.save(false)
		return n, nil
	}

	// Save when we're finished, if needed.
	save := false
	defer func() {
		if save {
			existing.save(false)
		}
	}()

	existing.Lock()
	defer existing.Unlock()

	if existing.State == Executed {
		return existing, fmt.Errorf("action already executed")
	}

	// check if the notification has been marked as
	// "executed externally".
	if n.State == Executed {
		log.Tracef("notifications: action for %s executed externally", n.EventID)
		existing.State = Executed
		save = true

		// in case the action has been executed immediately by the
		// sender we may need to update the SelectedActionID.
		// Though, we guard the assignments with value check
		// so partial updates that only change the
		// State property do not overwrite existing values.
		if n.SelectedActionID != "" {
			existing.SelectedActionID = n.SelectedActionID
		}
	}

	if n.SelectedActionID != "" && existing.State == Active {
		log.Tracef("notifications: selected action for %s: %s", n.EventID, n.SelectedActionID)
		existing.selectAndExecuteAction(n.SelectedActionID)
		save = true
	}

	return existing, nil
}

// Delete deletes a record from the database.
func (s *StorageInterface) Delete(key string) error {
	// Get EventID from key.
	if !strings.HasPrefix(key, "all/") {
		return storage.ErrNotFound
	}
	key = strings.TrimPrefix(key, "all/")

	// Get notification from storage.
	n, ok := getNotification(key)
	if !ok {
		return storage.ErrNotFound
	}

	n.delete(true)
	return nil
}

// ReadOnly returns whether the database is read only.
func (s *StorageInterface) ReadOnly() bool {
	return false
}

// EnsureNotification ensures that the given record is a Notification and returns it.
func EnsureNotification(r record.Record) (*Notification, error) {
	// unwrap
	if r.IsWrapped() {
		// only allocate a new struct, if we need it
		n := &Notification{}
		err := record.Unwrap(r, n)
		if err != nil {
			return nil, err
		}
		return n, nil
	}

	// or adjust type
	n, ok := r.(*Notification)
	if !ok {
		return nil, fmt.Errorf("record not of type *Notification, but %T", r)
	}
	return n, nil
}
