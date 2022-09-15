package network

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/safing/portbase/database"
	"github.com/safing/portbase/database/iterator"
	"github.com/safing/portbase/database/query"
	"github.com/safing/portbase/database/record"
	"github.com/safing/portbase/database/storage"
	"github.com/safing/portmaster/process"
)

const (
	dbScopeNone = ""
	dbScopeDNS  = "dns"
	dbScopeIP   = "ip"
)

var (
	dbController *database.Controller

	dnsConns = newConnectionStore()
	conns    = newConnectionStore()
)

// StorageInterface provides a storage.Interface to the
// configuration manager.
type StorageInterface struct {
	storage.InjectBase
}

// Database prefixes:
// Processes:       network:tree/<PID>
// DNS Requests:    network:tree/<PID>/dns/<ID>
// IP Connections:  network:tree/<PID>/ip/<ID>

func makeKey(pid int, scope, id string) string {
	if scope == "" {
		return "network:tree/" + strconv.Itoa(pid)
	}
	return fmt.Sprintf("network:tree/%d/%s/%s", pid, scope, id)
}

func parseDBKey(key string) (pid int, scope, id string, ok bool) {
	// Split into segments.
	segments := strings.Split(key, "/")

	// Keys have 2 or 4 segments.
	switch len(segments) {
	case 4:
		id = segments[3]

		fallthrough
	case 3:
		scope = segments[2]
		// Sanity check.
		switch scope {
		case dbScopeNone, dbScopeDNS, dbScopeIP:
			// Parsed id matches possible values.
			// The empty string is for matching a trailing slash for in query prefix.
			// TODO: For queries, also prefixes of these values are valid.
		default:
			// Unknown scope.
			return 0, "", "", false
		}

		fallthrough
	case 2:
		var err error
		if segments[1] == "" {
			pid = process.UndefinedProcessID
		} else {
			pid, err = strconv.Atoi(segments[1])
			if err != nil {
				return 0, "", "", false
			}
		}

		return pid, scope, id, true
	case 1:
		// This is a valid query prefix, but not process ID was given.
		return process.UndefinedProcessID, "", "", true
	default:
		return 0, "", "", false
	}
}

// Get returns a database record.
func (s *StorageInterface) Get(key string) (record.Record, error) {
	// Parse key and check if valid.
	pid, scope, id, ok := parseDBKey(strings.TrimPrefix(key, "network:"))
	if !ok || pid == process.UndefinedProcessID {
		return nil, storage.ErrNotFound
	}

	switch scope {
	case dbScopeDNS:
		if r, ok := dnsConns.get(id); ok {
			return r, nil
		}
	case dbScopeIP:
		if r, ok := conns.get(id); ok {
			return r, nil
		}
	case dbScopeNone:
		if proc, ok := process.GetProcessFromStorage(pid); ok {
			return proc, nil
		}
	}

	return nil, storage.ErrNotFound
}

// Query returns a an iterator for the supplied query.
func (s *StorageInterface) Query(q *query.Query, local, internal bool) (*iterator.Iterator, error) {
	it := iterator.New()

	module.StartWorker("connection query", func(_ context.Context) error {
		s.processQuery(q, it)
		return nil
	})

	return it, nil
}

func (s *StorageInterface) processQuery(q *query.Query, it *iterator.Iterator) {
	var matches bool
	pid, scope, _, ok := parseDBKey(q.DatabaseKeyPrefix())
	if !ok {
		it.Finish(nil)
		return
	}

	if pid == process.UndefinedProcessID {
		// processes
		for _, proc := range process.All() {
			func() {
				proc.Lock()
				defer proc.Unlock()
				matches = q.Matches(proc)
			}()
			if matches {
				it.Next <- proc
			}
		}
	}

	if scope == dbScopeNone || scope == dbScopeDNS {
		// dns scopes only
		for _, dnsConn := range dnsConns.clone() {
			func() {
				dnsConn.Lock()
				defer dnsConn.Unlock()
				matches = q.Matches(dnsConn)
			}()
			if matches {
				it.Next <- dnsConn
			}
		}
	}

	if scope == dbScopeNone || scope == dbScopeIP {
		// connections
		for _, conn := range conns.clone() {
			func() {
				conn.Lock()
				defer conn.Unlock()
				matches = q.Matches(conn)
			}()
			if matches {
				it.Next <- conn
			}
		}
	}

	it.Finish(nil)
}

func registerAsDatabase() error {
	_, err := database.Register(&database.Database{
		Name:        "network",
		Description: "Network and Firewall Data",
		StorageType: "injected",
	})
	if err != nil {
		return err
	}

	controller, err := database.InjectDatabase("network", &StorageInterface{})
	if err != nil {
		return err
	}

	dbController = controller
	process.SetDBController(dbController)
	return nil
}
