package netquery

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/hashicorp/go-multierror"
	servertiming "github.com/mitchellh/go-server-timing"

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

	// TODO: Open database in start() phase.
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

	batchHander := &BatchQueryHandler{
		Database:  m.Store,
		IsDevMode: config.Concurrent.GetAsBool(config.CfgDevModeKey, false),
	}

	chartHandler := &ChartHandler{
		Database: m.Store,
	}

	if err := api.RegisterEndpoint(api.Endpoint{
		Name:        "Query Connections",
		Description: "Query the in-memory sqlite connection database.",
		Path:        "netquery/query",
		MimeType:    "application/json",
		Read:        api.PermitUser, // Needs read+write as the query is sent using POST data.
		Write:       api.PermitUser, // Needs read+write as the query is sent using POST data.
		BelongsTo:   m.Module,
		HandlerFunc: servertiming.Middleware(queryHander, nil).ServeHTTP,
	}); err != nil {
		return fmt.Errorf("failed to register API endpoint: %w", err)
	}

	if err := api.RegisterEndpoint(api.Endpoint{
		Name:        "Batch Query Connections",
		Description: "Batch query the in-memory sqlite connection database.",
		Path:        "netquery/query/batch",
		MimeType:    "application/json",
		Read:        api.PermitUser, // Needs read+write as the query is sent using POST data.
		Write:       api.PermitUser, // Needs read+write as the query is sent using POST data.
		BelongsTo:   m.Module,
		HandlerFunc: servertiming.Middleware(batchHander, nil).ServeHTTP,
	}); err != nil {
		return fmt.Errorf("failed to register API endpoint: %w", err)
	}

	if err := api.RegisterEndpoint(api.Endpoint{
		Name:        "Active Connections Chart",
		Description: "Query the in-memory sqlite connection database and return a chart of active connections.",
		Path:        "netquery/charts/connection-active",
		MimeType:    "application/json",
		Write:       api.PermitUser,
		BelongsTo:   m.Module,
		HandlerFunc: servertiming.Middleware(chartHandler, nil).ServeHTTP,
	}); err != nil {
		return fmt.Errorf("failed to register API endpoint: %w", err)
	}

	if err := api.RegisterEndpoint(api.Endpoint{
		Name:        "Remove connections from profile history",
		Description: "Remove all connections from the history database for one or more profiles",
		Path:        "netquery/history/clear",
		MimeType:    "application/json",
		Write:       api.PermitUser,
		BelongsTo:   m.Module,
		ActionFunc: func(ar *api.Request) (msg string, err error) {
			// TODO: Use query parameters instead.
			var body struct {
				ProfileIDs []string `json:"profileIDs"`
			}
			if err := json.Unmarshal(ar.InputData, &body); err != nil {
				return "", fmt.Errorf("failed to decode parameters in body: %w", err)
			}

			if len(body.ProfileIDs) == 0 {
				if err := m.mng.store.RemoveAllHistoryData(ar.Context()); err != nil {
					return "", fmt.Errorf("failed to remove all history: %w", err)
				}
			} else {
				merr := new(multierror.Error)
				for _, profileID := range body.ProfileIDs {
					if err := m.mng.store.RemoveHistoryForProfile(ar.Context(), profileID); err != nil {
						merr.Errors = append(merr.Errors, fmt.Errorf("failed to clear history for %q: %w", profileID, err))
					} else {
						log.Infof("netquery: successfully cleared history for %s", profileID)
					}
				}

				if err := merr.ErrorOrNil(); err != nil {
					return "", err
				}
			}

			return "Successfully cleared history.", nil
		},
	}); err != nil {
		return fmt.Errorf("failed to register API endpoint: %w", err)
	}

	if err := api.RegisterEndpoint(api.Endpoint{
		Name:      "Apply connection history retention threshold",
		Path:      "netquery/history/cleanup",
		Write:     api.PermitUser,
		BelongsTo: m.Module,
		ActionFunc: func(ar *api.Request) (msg string, err error) {
			if err := m.Store.CleanupHistory(ar.Context()); err != nil {
				return "", err
			}
			return "Deleted outdated connections.", nil
		},
	}); err != nil {
		return fmt.Errorf("failed to register API endpoint: %w", err)
	}

	return nil
}

func (m *module) start() error {
	m.StartServiceWorker("netquery connection feed listener", 0, func(ctx context.Context) error {
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

	m.StartServiceWorker("netquery connection feed handler", 0, func(ctx context.Context) error {
		m.mng.HandleFeed(ctx, m.feed)
		return nil
	})

	m.StartServiceWorker("netquery live db cleaner", 0, func(ctx context.Context) error {
		for {
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(10 * time.Second):
				threshold := time.Now().Add(-network.DeleteConnsAfterEndedThreshold)
				count, err := m.Store.Cleanup(ctx, threshold)
				if err != nil {
					log.Errorf("netquery: failed to removed old connections from live db: %s", err)
				} else {
					log.Tracef("netquery: successfully removed %d old connections from live db that ended before %s", count, threshold)
				}
			}
		}
	})

	m.NewTask("network history cleaner", func(ctx context.Context, _ *modules.Task) error {
		return m.Store.CleanupHistory(ctx)
	}).Repeat(time.Hour).Schedule(time.Now().Add(10 * time.Minute))

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

	if err := m.mng.store.Close(); err != nil {
		log.Errorf("netquery: failed to close sqlite database: %s", err)
	} else {
		// Clear deleted connections from database.
		if err := VacuumHistory(ctx); err != nil {
			log.Errorf("netquery: failed to execute VACUUM in history database: %s", err)
		}
	}

	return nil
}
