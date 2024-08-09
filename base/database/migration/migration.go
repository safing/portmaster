package migration

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/hashicorp/go-version"

	"github.com/safing/portmaster/base/database"
	"github.com/safing/portmaster/base/database/record"
	"github.com/safing/portmaster/base/log"
	"github.com/safing/structures/dsd"
)

// MigrateFunc is called when a migration should be applied to the
// database. It receives the current version (from) and the target
// version (to) of the database and a dedicated interface for
// interacting with data stored in the DB.
// A dedicated log.ContextTracer is added to ctx for each migration
// run.
type MigrateFunc func(ctx context.Context, from, to *version.Version, dbInterface *database.Interface) error

// Migration represents a registered data-migration that should be applied to
// some database. Migrations are stacked on top and executed in order of increasing
// version number (see Version field).
type Migration struct {
	// Description provides a short human-readable description of the
	// migration.
	Description string
	// Version should hold the version of the database/subsystem after
	// the migration has been applied.
	Version string
	// MigrateFuc is executed when the migration should be performed.
	MigrateFunc MigrateFunc
}

// Registry holds a migration stack.
type Registry struct {
	key string

	lock       sync.Mutex
	migrations []Migration
}

// New creates a new migration registry.
// The key should be the name of the database key that is used to store
// the version of the last successfully applied migration.
func New(key string) *Registry {
	return &Registry{
		key: key,
	}
}

// Add adds one or more migrations to reg.
func (reg *Registry) Add(migrations ...Migration) error {
	reg.lock.Lock()
	defer reg.lock.Unlock()
	for _, m := range migrations {
		if _, err := version.NewSemver(m.Version); err != nil {
			return fmt.Errorf("migration %q: invalid version %s: %w", m.Description, m.Version, err)
		}
		reg.migrations = append(reg.migrations, m)
	}
	return nil
}

// Migrate migrates the database by executing all registered
// migration in order of increasing version numbers. The error
// returned, if not nil, is always of type *Diagnostics.
func (reg *Registry) Migrate(ctx context.Context) (err error) {
	reg.lock.Lock()
	defer reg.lock.Unlock()

	start := time.Now()
	log.Infof("migration: migration of %s started", reg.key)
	defer func() {
		if err != nil {
			log.Errorf("migration: migration of %s failed after %s: %s", reg.key, time.Since(start), err)
		} else {
			log.Infof("migration: migration of %s finished after %s", reg.key, time.Since(start))
		}
	}()

	db := database.NewInterface(&database.Options{
		Local:    true,
		Internal: true,
	})

	startOfMigration, err := reg.getLatestSuccessfulMigration(db)
	if err != nil {
		return err
	}

	execPlan, diag, err := reg.getExecutionPlan(startOfMigration)
	if err != nil {
		return err
	}
	if len(execPlan) == 0 {
		return nil
	}
	diag.TargetVersion = execPlan[len(execPlan)-1].Version

	// finally, apply our migrations
	lastAppliedMigration := startOfMigration
	for _, m := range execPlan {
		target, _ := version.NewSemver(m.Version) // we can safely ignore the error here

		migrationCtx, tracer := log.AddTracer(ctx)

		if err := m.MigrateFunc(migrationCtx, lastAppliedMigration, target, db); err != nil {
			diag.Wrapped = err
			diag.FailedMigration = m.Description
			tracer.Errorf("migration: migration for %s failed: %s - %s", reg.key, target.String(), m.Description)
			tracer.Submit()
			return diag
		}

		lastAppliedMigration = target
		diag.LastSuccessfulMigration = lastAppliedMigration.String()

		if err := reg.saveLastSuccessfulMigration(db, target); err != nil {
			diag.Message = "failed to persist migration status"
			diag.Wrapped = err
			diag.FailedMigration = m.Description
		}
		tracer.Infof("migration: applied migration for %s: %s - %s", reg.key, target.String(), m.Description)
		tracer.Submit()
	}

	// all migrations have been applied successfully, we're done here
	return nil
}

func (reg *Registry) getLatestSuccessfulMigration(db *database.Interface) (*version.Version, error) {
	// find the latest version stored in the database
	rec, err := db.Get(reg.key)
	if errors.Is(err, database.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, &Diagnostics{
			Message: "failed to query database for migration status",
			Wrapped: err,
		}
	}

	// Unwrap the record to get the actual database
	r, ok := rec.(*record.Wrapper)
	if !ok {
		return nil, &Diagnostics{
			Wrapped: errors.New("expected wrapped database record"),
		}
	}

	sv, err := version.NewSemver(string(r.Data))
	if err != nil {
		return nil, &Diagnostics{
			Message: "failed to parse version stored in migration status record",
			Wrapped: err,
		}
	}
	return sv, nil
}

func (reg *Registry) saveLastSuccessfulMigration(db *database.Interface, ver *version.Version) error {
	r := &record.Wrapper{
		Data:   []byte(ver.String()),
		Format: dsd.RAW,
	}
	r.SetKey(reg.key)

	return db.Put(r)
}

func (reg *Registry) getExecutionPlan(startOfMigration *version.Version) ([]Migration, *Diagnostics, error) {
	// create a look-up map for migrations indexed by their semver created a
	// list of version (sorted by increasing number) that we use as our execution
	// plan.
	lm := make(map[string]Migration)
	versions := make(version.Collection, 0, len(reg.migrations))
	for _, m := range reg.migrations {
		ver, err := version.NewSemver(m.Version)
		if err != nil {
			return nil, nil, &Diagnostics{
				Message:         "failed to parse version of migration",
				Wrapped:         err,
				FailedMigration: m.Description,
			}
		}
		lm[ver.String()] = m // use .String() for a normalized string representation
		versions = append(versions, ver)
	}
	sort.Sort(versions)

	diag := new(Diagnostics)
	if startOfMigration != nil {
		diag.StartOfMigration = startOfMigration.String()
	}

	// prepare our diagnostics and the execution plan
	execPlan := make([]Migration, 0, len(versions))
	for _, ver := range versions {
		// skip an migration that has already been applied.
		if startOfMigration != nil && startOfMigration.GreaterThanOrEqual(ver) {
			continue
		}
		m := lm[ver.String()]
		diag.ExecutionPlan = append(diag.ExecutionPlan, DiagnosticStep{
			Description: m.Description,
			Version:     ver.String(),
		})
		execPlan = append(execPlan, m)
	}

	return execPlan, diag, nil
}
