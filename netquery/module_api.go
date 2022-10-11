package netquery

import (
	"context"
	"fmt"
	"time"

	"github.com/safing/portbase/api"
	"github.com/safing/portbase/config"
	"github.com/safing/portbase/database"
	"github.com/safing/portbase/database/query"
	"github.com/safing/portbase/log"
	"github.com/safing/portbase/modules"
	"github.com/safing/portbase/runtime"
	"github.com/safing/portmaster/network"
)

type module struct {
	*modules.Module

	db       *database.Interface
	sqlStore *Database
	mng      *Manager
	feed     chan *network.Connection
}

func init() {
	m := new(module)
	m.Module = modules.Register(
		"netquery",
		m.prepare,
		m.start,
		m.stop,
		"api",
		"network",
		"database",
	)
}

func (m *module) prepare() error {
	var err error

	m.db = database.NewInterface(&database.Options{
		Local:    true,
		Internal: true,
	})

	m.sqlStore, err = NewInMemory()
	if err != nil {
		return fmt.Errorf("failed to create in-memory database: %w", err)
	}

	m.mng, err = NewManager(m.sqlStore, "netquery/data/", runtime.DefaultRegistry)
	if err != nil {
		return fmt.Errorf("failed to create manager: %w", err)
	}

	m.feed = make(chan *network.Connection, 1000)

	queryHander := &QueryHandler{
		Database:  m.sqlStore,
		IsDevMode: config.Concurrent.GetAsBool(config.CfgDevModeKey, false),
	}

	chartHandler := &ChartHandler{
		Database: m.sqlStore,
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
				count, err := m.sqlStore.Cleanup(ctx, threshold)
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
		_, err := NewRuntimeQueryRunner(m.sqlStore, "netquery/query/", runtime.DefaultRegistry)
		if err != nil {
			return fmt.Errorf("failed to set up runtime SQL query runner: %w", err)
		}
	}

	return nil
}

func (m *module) stop() error {
	return nil
}
