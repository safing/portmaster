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

func init() {
	var (
		module   *modules.Module
		db       *database.Interface
		sqlStore *Database
		mng      *Manager
	)

	module = modules.Register(
		"netquery",
		/* Prepare Module */
		func() error {
			var err error

			db = database.NewInterface(&database.Options{
				Local:     true,
				Internal:  true,
				CacheSize: 0,
			})

			sqlStore, err = NewInMemory()
			if err != nil {
				return fmt.Errorf("failed to create in-memory database: %w", err)
			}

			mng, err = NewManager(sqlStore, "netquery/updates/", runtime.DefaultRegistry)
			if err != nil {
				return fmt.Errorf("failed to create manager: %w", err)
			}

			return nil
		},
		/* Start Module */
		func() error {
			ch := make(chan *network.Connection, 100)

			module.StartServiceWorker("netquery-feeder", time.Second, func(ctx context.Context) error {
				sub, err := db.Subscribe(query.New("network:"))
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

						ch <- conn
					}
				}
			})

			module.StartServiceWorker("netquery-persister", time.Second, func(ctx context.Context) error {
				defer close(ch)

				mng.HandleFeed(ctx, ch)
				return nil
			})

			module.StartWorker("netquery-row-cleaner", func(ctx context.Context) error {
				for {
					select {
					case <-ctx.Done():
						return nil
					case <-time.After(10 * time.Second):
						count, err := sqlStore.Cleanup(ctx, time.Now().Add(-5*time.Minute))
						if err != nil {
							log.Errorf("netquery: failed to count number of rows in memory: %w", err)
						} else {
							log.Infof("netquery: successfully removed %d old rows", count)
						}
					}
				}
			})

			module.StartWorker("netquery-row-counter", func(ctx context.Context) error {
				for {
					select {
					case <-ctx.Done():
						return nil
					case <-time.After(5 * time.Second):
						count, err := sqlStore.CountRows(ctx)
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
			_, err := NewRuntimeQueryRunner(sqlStore, "netquery/query/", runtime.DefaultRegistry)
			if err != nil {
				return fmt.Errorf("failed to set up runtime SQL query runner: %w", err)
			}

			return nil
		},
		nil,
		"network",
		"database",
	)

	module.Enable()
}
