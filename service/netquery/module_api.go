package netquery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/hashicorp/go-multierror"
	servertiming "github.com/mitchellh/go-server-timing"

	"github.com/safing/portmaster/base/api"
	"github.com/safing/portmaster/base/config"
	"github.com/safing/portmaster/base/database"
	"github.com/safing/portmaster/base/database/query"
	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/base/runtime"
	"github.com/safing/portmaster/service/mgr"
	"github.com/safing/portmaster/service/network"
	"github.com/safing/portmaster/service/profile"
)

type NetQuery struct {
	mgr      *mgr.Manager
	instance instance

	Store *Database

	db   *database.Interface
	mng  *Manager
	feed chan *network.Connection
}

func (nq *NetQuery) prepare() error {
	var err error

	nq.db = database.NewInterface(&database.Options{
		Local:    true,
		Internal: true,
	})

	// TODO: Open database in start() phase.
	nq.Store, err = NewInMemory()
	if err != nil {
		return fmt.Errorf("failed to create in-memory database: %w", err)
	}

	nq.mng, err = NewManager(nq.Store, "netquery/data/", runtime.DefaultRegistry)
	if err != nil {
		return fmt.Errorf("failed to create manager: %w", err)
	}

	nq.feed = make(chan *network.Connection, 1000)

	queryHander := &QueryHandler{
		Database:  nq.Store,
		IsDevMode: config.Concurrent.GetAsBool(config.CfgDevModeKey, false),
	}

	batchHander := &BatchQueryHandler{
		Database:  nq.Store,
		IsDevMode: config.Concurrent.GetAsBool(config.CfgDevModeKey, false),
	}

	chartHandler := &ActiveChartHandler{
		Database: nq.Store,
	}

	bwChartHandler := &BandwidthChartHandler{
		Database: nq.Store,
	}

	if err := api.RegisterEndpoint(api.Endpoint{
		Name:        "Query Connections",
		Description: "Query the in-memory sqlite connection database.",
		Path:        "netquery/query",
		MimeType:    "application/json",
		Read:        api.PermitUser, // Needs read+write as the query is sent using POST data.
		Write:       api.PermitUser, // Needs read+write as the query is sent using POST data.
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
		HandlerFunc: servertiming.Middleware(chartHandler, nil).ServeHTTP,
	}); err != nil {
		return fmt.Errorf("failed to register API endpoint: %w", err)
	}

	if err := api.RegisterEndpoint(api.Endpoint{
		// TODO: Use query parameters instead.
		Path:        "netquery/charts/bandwidth",
		MimeType:    "application/json",
		Write:       api.PermitUser,
		HandlerFunc: bwChartHandler.ServeHTTP,
		Name:        "Bandwidth Chart",
		Description: "Query the in-memory sqlite connection database and return a chart of bytes sent/received.",
	}); err != nil {
		return fmt.Errorf("failed to register API endpoint: %w", err)
	}

	if err := api.RegisterEndpoint(api.Endpoint{
		Name:        "Remove connections from profile history",
		Description: "Remove all connections from the history database for one or more profiles",
		Path:        "netquery/history/clear",
		MimeType:    "application/json",
		Write:       api.PermitUser,
		ActionFunc: func(ar *api.Request) (msg string, err error) {
			var body struct {
				ProfileIDs []string `json:"profileIDs"`
			}
			if err := json.Unmarshal(ar.InputData, &body); err != nil {
				return "", fmt.Errorf("failed to decode parameters in body: %w", err)
			}

			if len(body.ProfileIDs) == 0 {
				if err := nq.mng.store.RemoveAllHistoryData(ar.Context()); err != nil {
					return "", fmt.Errorf("failed to remove all history: %w", err)
				}
			} else {
				merr := new(multierror.Error)
				for _, profileID := range body.ProfileIDs {
					if err := nq.mng.store.RemoveHistoryForProfile(ar.Context(), profileID); err != nil {
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
		Name:  "Apply connection history retention threshold",
		Path:  "netquery/history/cleanup",
		Write: api.PermitUser,
		ActionFunc: func(ar *api.Request) (msg string, err error) {
			if err := nq.Store.CleanupHistory(ar.Context()); err != nil {
				return "", err
			}
			return "Deleted outdated connections.", nil
		},
	}); err != nil {
		return fmt.Errorf("failed to register API endpoint: %w", err)
	}

	return nil
}

func (nq *NetQuery) Manager() *mgr.Manager {
	return nq.mgr
}

func (nq *NetQuery) Start() error {
	nq.mgr.Go("netquery connection feed listener", func(ctx *mgr.WorkerCtx) error {
		sub, err := nq.db.Subscribe(query.New("network:"))
		if err != nil {
			return fmt.Errorf("failed to subscribe to network tree: %w", err)
		}
		defer close(nq.feed)
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

				nq.feed <- conn
			}
		}
	})

	nq.mgr.Go("netquery connection feed handler", func(ctx *mgr.WorkerCtx) error {
		nq.mng.HandleFeed(ctx.Ctx(), nq.feed)
		return nil
	})

	nq.mgr.Go("netquery live db cleaner", func(ctx *mgr.WorkerCtx) error {
		for {
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(10 * time.Second):
				threshold := time.Now().Add(-network.DeleteConnsAfterEndedThreshold)
				count, err := nq.Store.Cleanup(ctx.Ctx(), threshold)
				if err != nil {
					log.Errorf("netquery: failed to removed old connections from live db: %s", err)
				} else {
					log.Tracef("netquery: successfully removed %d old connections from live db that ended before %s", count, threshold)
				}
			}
		}
	})

	nq.mgr.Delay("network history cleaner delay", 10*time.Minute, func(w *mgr.WorkerCtx) error {
		return nq.Store.CleanupHistory(w.Ctx())
	}).Repeat(1 * time.Hour)

	// For debugging, provide a simple direct SQL query interface using
	// the runtime database.
	// Only expose in development mode.
	if config.GetAsBool(config.CfgDevModeKey, false)() {
		_, err := NewRuntimeQueryRunner(nq.Store, "netquery/query/", runtime.DefaultRegistry)
		if err != nil {
			return fmt.Errorf("failed to set up runtime SQL query runner: %w", err)
		}
	}

	// Migrate profile IDs in history database when profiles are migrated/merged.
	nq.instance.Profile().EventMigrated.AddCallback("migrate profile IDs in history database",
		func(ctx *mgr.WorkerCtx, profileIDs []string) (bool, error) {
			if len(profileIDs) == 2 {
				return false, nq.Store.MigrateProfileID(ctx.Ctx(), profileIDs[0], profileIDs[1])
			}
			return false, nil
		})

	return nil
}

func (nq *NetQuery) Stop() error {
	// Cacnel the module context.
	nq.mgr.Cancel()
	// Wait for all workers before we start the shutdown.
	nq.mgr.WaitForWorkersFromStop(time.Minute)

	// we don't use the module ctx here because it is already canceled.
	// just give the clean up 1 minute to happen and abort otherwise.
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	if err := nq.mng.store.MarkAllHistoryConnectionsEnded(ctx); err != nil {
		// handle the error by just logging it. There's not much we can do here
		// and returning an error to the module system doesn't help much as well...
		log.Errorf("netquery: failed to mark connections in history database as ended: %s", err)
	}

	if err := nq.mng.store.Close(); err != nil {
		log.Errorf("netquery: failed to close sqlite database: %s", err)
	} else {
		// Clear deleted connections from database.
		if err := VacuumHistory(ctx); err != nil {
			log.Errorf("netquery: failed to execute VACUUM in history database: %s", err)
		}
	}

	return nil
}

var (
	module     *NetQuery
	shimLoaded atomic.Bool
)

// NewModule returns a new NetQuery module.
func NewModule(instance instance) (*NetQuery, error) {
	if !shimLoaded.CompareAndSwap(false, true) {
		return nil, errors.New("only one instance allowed")
	}
	m := mgr.New("NetQuery")
	module = &NetQuery{
		mgr:      m,
		instance: instance,
	}
	if err := module.prepare(); err != nil {
		return nil, fmt.Errorf("failed to prepare netquery module: %w", err)
	}
	return module, nil
}

type instance interface {
	Profile() *profile.ProfileModule
}
