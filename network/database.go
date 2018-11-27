package network

import (
	"strconv"
	"strings"
	"sync"

	"github.com/Safing/portbase/database"
	"github.com/Safing/portbase/database/iterator"
	"github.com/Safing/portbase/database/query"
	"github.com/Safing/portbase/database/record"
	"github.com/Safing/portbase/database/storage"
	"github.com/Safing/portmaster/process"
)

var (
	links       map[string]*Link
	connections map[string]*Connection
	dataLock    sync.RWMutex

	dbController *database.Controller
)

// StorageInterface provices a storage.Interface to the configuration manager.
type StorageInterface struct {
	storage.InjectBase
}

// Get returns a database record.
func (s *StorageInterface) Get(key string) (record.Record, error) {

	dataLock.RLock()
	defer dataLock.RUnlock()

	splitted := strings.Split(key, "/")
	switch splitted[0] {
	case "tree":
		switch len(splitted) {
		case 2:
			pid, err := strconv.Atoi(splitted[1])
			if err != nil {
				return process.GetProcessByPID(pid)
			}
		case 3:
			conn, ok := connections[splitted[2]]
			if ok {
				return conn, nil
			}
		case 4:
			link, ok := links[splitted[3]]
			if ok {
				return link, nil
			}
		}
	}

	return nil, storage.ErrNotFound
}

// Query returns a an iterator for the supplied query.
func (s *StorageInterface) Query(q *query.Query, local, internal bool) (*iterator.Iterator, error) {
	it := iterator.New()
	go s.processQuery(q, it)
	// TODO: check local and internal

	return it, nil
}

func (s *StorageInterface) processQuery(q *query.Query, it *iterator.Iterator) {
	// processes
	for _, proc := range process.All() {
		if strings.HasPrefix(proc.Meta().DatabaseKey, q.DatabaseKeyPrefix()) {
			it.Next <- proc
		}
	}

	dataLock.RLock()
	defer dataLock.RUnlock()

	// connections
	for _, conn := range connections {
		if strings.HasPrefix(conn.Meta().DatabaseKey, q.DatabaseKeyPrefix()) {
			it.Next <- conn
		}
	}

	// links
	for _, link := range links {
		if strings.HasPrefix(opt.Meta().DatabaseKey, q.DatabaseKeyPrefix()) {
			it.Next <- link
		}
	}

	it.Finish(nil)
}

func registerAsDatabase() error {
	_, err := database.Register(&database.Database{
		Name:        "network",
		Description: "Network and Firewall Data",
		StorageType: "injected",
		PrimaryAPI:  "",
	})
	if err != nil {
		return err
	}

	controller, err := database.InjectDatabase("network", &ConfigStorageInterface{})
	if err != nil {
		return err
	}

	dbController = controller
	process.SetDBController(dbController)
	return nil
}
