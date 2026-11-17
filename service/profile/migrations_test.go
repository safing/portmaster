package profile

import (
	"context"
	"errors"
	"testing"

	"github.com/hashicorp/go-version"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/safing/portmaster/base/database"
	"github.com/safing/portmaster/base/database/iterator"
	"github.com/safing/portmaster/base/database/query"
	"github.com/safing/portmaster/base/database/storage"
)

// abortingStorage is a storage whose queries start successfully, but whose
// iteration is aborted with an error before any record is delivered.
type abortingStorage struct {
	storage.InjectBase
}

var errIterationAborted = errors.New("iteration aborted")

func (*abortingStorage) Query(_ *query.Query, _, _ bool) (*iterator.Iterator, error) {
	it := iterator.New()
	it.Finish(errIterationAborted)
	return it, nil
}

// TestMigrationsReportQueryStartFailure verifies that migrations return an
// error if the profile query cannot be started, so that the migration is not
// recorded as applied and is retried on the next start.
func TestMigrationsReportQueryStartFailure(t *testing.T) {
	t.Parallel()

	// The profile database is not available in this test, so every query fails.
	db := database.NewInterface(&database.Options{Local: true, Internal: true})
	to := version.Must(version.NewSemver("v1.6.3"))

	for name, migrate := range map[string]func(context.Context, *version.Version, *version.Version, *database.Interface) error{
		"linked path": migrateLinkedPath,
		"icons":       migrateIcons,
		"derived IDs": migrateToDerivedIDs,
	} {
		err := migrate(context.Background(), nil, to, db)
		assert.Error(t, err, "migration %q must fail if the query cannot be started", name)
	}
}

// TestMigrationsReportIterationFailure verifies that migrations return an
// error if the profile iteration is aborted midway, so that the migration is
// not recorded as applied and the remaining profiles are migrated on retry.
//
//nolint:paralleltest // Initializes and injects into the global database system.
func TestMigrationsReportIterationFailure(t *testing.T) {
	require.NoError(t, database.Initialize(t.TempDir()))
	_, err := database.Register(&database.Database{
		Name:        "core",
		Description: "test",
		StorageType: database.StorageTypeInjected,
	})
	require.NoError(t, err)
	controller, err := database.InjectDatabase("core", &abortingStorage{})
	require.NoError(t, err)
	t.Cleanup(controller.Withdraw)

	db := database.NewInterface(&database.Options{Local: true, Internal: true})
	to := version.Must(version.NewSemver("v1.6.3"))

	for name, migrate := range map[string]func(context.Context, *version.Version, *version.Version, *database.Interface) error{
		"linked path": migrateLinkedPath,
		"icons":       migrateIcons,
		"derived IDs": migrateToDerivedIDs,
	} {
		err := migrate(context.Background(), nil, to, db)
		require.ErrorIs(t, err, errIterationAborted, "migration %q must fail if the iteration is aborted", name)
	}
}
