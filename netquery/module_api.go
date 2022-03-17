package netquery

import (
	"context"
	"fmt"
	"time"

	"github.com/safing/portbase/database"
	"github.com/safing/portbase/database/query"
	"github.com/safing/portbase/log"
	"github.com/safing/portbase/modules"
	"github.com/safing/portbase/runtime"
	"github.com/safing/portmaster/network"
)

type Module struct {
	*modules.Module

	db       *database.Interface
	sqlStore *Database
	mng      *Manager
	feed     chan *network.Connection
}

func init() {
	mod := new(Module)
	mod.Module = modules.Register(
		"netquery",
		mod.Prepare,
		mod.Start,
		mod.Stop,
		"network",
		"database",
	)
}

func (m *Module) Prepare() error {
	var err error

	m.db = database.NewInterface(&database.Options{
		Local:     true,
		Internal:  true,
		CacheSize: 0,
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

	return nil
}

func (mod *Module) Start() error {
	mod.StartServiceWorker("netquery-feeder", time.Second, func(ctx context.Context) error {
		sub, err := mod.db.Subscribe(query.New("network:"))
		if err != nil {
			return fmt.Errorf("failed to subscribe to network tree: %w", err)
		}
		defer sub.Cancel()

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

				mod.feed <- conn
			}
		}
	})

	mod.StartServiceWorker("netquery-persister", time.Second, func(ctx context.Context) error {
		mod.mng.HandleFeed(ctx, mod.feed)
		return nil
	})

	mod.StartServiceWorker("netquery-row-cleaner", time.Second, func(ctx context.Context) error {
		for {
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(10 * time.Second):
				count, err := mod.sqlStore.Cleanup(ctx, time.Now().Add(-network.DeleteConnsAfterEndedThreshold))
				if err != nil {
					log.Errorf("netquery: failed to count number of rows in memory: %w", err)
				} else {
					log.Infof("netquery: successfully removed %d old rows", count)
				}
			}
		}
	})

	mod.StartWorker("netquery-row-counter", func(ctx context.Context) error {
		for {
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(5 * time.Second):
				count, err := mod.sqlStore.CountRows(ctx)
				if err != nil {
					log.Errorf("netquery: failed to count number of rows in memory: %w", err)
				} else {
					log.Infof("netquery: currently holding %d rows in memory", count)
				}

				/*
					if err := sqlStore.dumpTo(ctx, os.Stderr); err != nil {
						log.Errorf("netquery: failed to dump sqlite memory content: %w", err)
					}
				*/
			}
		}
	})

	// for debugging, we provide a simple direct SQL query interface using
	// the runtime database
	_, err := NewRuntimeQueryRunner(mod.sqlStore, "netquery/query/", runtime.DefaultRegistry)
	if err != nil {
		return fmt.Errorf("failed to set up runtime SQL query runner: %w", err)
	}

	return nil
}

func (mod *Module) Stop() error {
	close(mod.feed)

	return nil
}
