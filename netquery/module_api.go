package netquery

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/hashicorp/go-multierror"

	"github.com/safing/portbase/api"
	"github.com/safing/portbase/config"
	"github.com/safing/portbase/database"
	"github.com/safing/portbase/database/query"
	"github.com/safing/portbase/log"
	"github.com/safing/portbase/modules"
	"github.com/safing/portbase/modules/subsystems"
	"github.com/safing/portbase/runtime"
	"github.com/safing/portmaster/network"
)

// DefaultModule is the default netquery module.
var DefaultModule *module

type module struct {
	*modules.Module

	Store *Database

	db   *database.Interface
	mng  *Manager
	feed chan *network.Connection
}

func init() {
	DefaultModule = new(module)

	DefaultModule.Module = modules.Register(
		"netquery",
		DefaultModule.prepare,
		DefaultModule.start,
		DefaultModule.stop,
		"api",
		"network",
		"database",
	)

	subsystems.Register(
		"history",
		"Network History",
		"Keep Network History Data",
		DefaultModule.Module,
		"config:history/",
		nil,
	)
}

func (m *module) prepare() error {
	var err error

	m.db = database.NewInterface(&database.Options{
		Local:    true,
		Internal: true,
	})

	m.Store, err = NewInMemory()
	if err != nil {
		return fmt.Errorf("failed to create in-memory database: %w", err)
	}

	m.mng, err = NewManager(m.Store, "netquery/data/", runtime.DefaultRegistry)
	if err != nil {
		return fmt.Errorf("failed to create manager: %w", err)
	}

	m.feed = make(chan *network.Connection, 1000)

	queryHander := &QueryHandler{
		Database:  m.Store,
		IsDevMode: config.Concurrent.GetAsBool(config.CfgDevModeKey, false),
	}

	chartHandler := &ChartHandler{
		Database: m.Store,
	}

	if err := api.RegisterEndpoint(api.Endpoint{
		Path:        "netquery/query",
		MimeType:    "application/json",
		Read:        api.PermitUser,
		Write:       api.PermitUser,
		BelongsTo:   m.Module,
		HandlerFunc: queryHander.ServeHTTP,
		Name:        "Query Connections",
		Description: "Query the in-memory sqlite connection database.",
	}); err != nil {
		return fmt.Errorf("failed to register API endpoint: %w", err)
	}

	if err := api.RegisterEndpoint(api.Endpoint{
		Path:        "netquery/charts/connection-active",
		MimeType:    "application/json",
		Read:        api.PermitUser,
		Write:       api.PermitUser,
		BelongsTo:   m.Module,
		HandlerFunc: chartHandler.ServeHTTP,
		Name:        "Active Connections Chart",
		Description: "Query the in-memory sqlite connection database and return a chart of active connections.",
	}); err != nil {
		return fmt.Errorf("failed to register API endpoint: %w", err)
	}

	if err := api.RegisterEndpoint(api.Endpoint{
		Path:      "netquery/history/clear",
		MimeType:  "application/json",
		Read:      api.PermitUser,
		Write:     api.PermitUser,
		BelongsTo: m.Module,
		HandlerFunc: func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				ProfileIDs []string `json:"profileIDs"`
			}

			dec := json.NewDecoder(r.Body)
			dec.DisallowUnknownFields()

			if err := dec.Decode(&body); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}

			if len(body.ProfileIDs) == 0 {
				if err := m.mng.store.RemoveAllHistoryData(r.Context()); err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)

					return
				}
			} else {
				merr := new(multierror.Error)
				for _, profileID := range body.ProfileIDs {
					if err := m.mng.store.RemoveHistoryForProfile(r.Context(), profileID); err != nil {
						merr.Errors = append(merr.Errors, fmt.Errorf("failed to clear history for %q: %w", profileID, err))
					} else {
						log.Infof("netquery: successfully cleared history for %s", profileID)
					}
				}

				if err := merr.ErrorOrNil(); err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)

					return
				}
			}

			w.WriteHeader(http.StatusNoContent)
		},
		Name:        "Remove connections from profile history",
		Description: "Remove all connections from the history database for one or more profiles",
	}); err != nil {
		return fmt.Errorf("failed to register API endpoint: %w", err)
	}

	return nil
}

func (m *module) start() error {
	m.StartServiceWorker("netquery-feeder", time.Second, func(ctx context.Context) error {
		sub, err := m.db.Subscribe(query.New("network:"))
		if err != nil {
			return fmt.Errorf("failed to subscribe to network tree: %w", err)
		}
		defer close(m.feed)
		defer func() {
			_ = sub.Cancel()
		}()

		for {
			select {
			case <-ctx.Done():
				return nil
			case rec, ok := <-sub.Feed:
				if !ok {
					return nil
				}

				conn, ok := rec.(*network.Connection)
				if !ok {
					// This is fine as we also receive process updates on
					// this channel.
					continue
				}

				m.feed <- conn
			}
		}
	})

	m.StartServiceWorker("netquery-persister", time.Second, func(ctx context.Context) error {
		m.mng.HandleFeed(ctx, m.feed)
		return nil
	})

	m.StartServiceWorker("netquery-row-cleaner", time.Second, func(ctx context.Context) error {
		for {
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(10 * time.Second):
				threshold := time.Now().Add(-network.DeleteConnsAfterEndedThreshold)
				count, err := m.Store.Cleanup(ctx, threshold)
				if err != nil {
					log.Errorf("netquery: failed to count number of rows in memory: %s", err)
				} else {
					log.Tracef("netquery: successfully removed %d old rows that ended before %s", count, threshold)
				}
			}
		}
	})

	// For debugging, provide a simple direct SQL query interface using
	// the runtime database.
	// Only expose in development mode.
	if config.GetAsBool(config.CfgDevModeKey, false)() {
		_, err := NewRuntimeQueryRunner(m.Store, "netquery/query/", runtime.DefaultRegistry)
		if err != nil {
			return fmt.Errorf("failed to set up runtime SQL query runner: %w", err)
		}
	}

	return nil
}

func (m *module) stop() error {
	// we don't use m.Module.Ctx here because it is already cancelled when stop is called.
	// just give the clean up 1 minute to happen and abort otherwise.
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	if err := m.mng.store.MarkAllHistoryConnectionsEnded(ctx); err != nil {
		// handle the error by just logging it. There's not much we can do here
		// and returning an error to the module system doesn't help much as well...
		log.Errorf("netquery: failed to mark connections in history database as ended: %s", err)
	}

	return nil
}
