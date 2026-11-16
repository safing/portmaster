package profile

import (
	"context"
	"testing"

	"github.com/hashicorp/go-version"
	"github.com/stretchr/testify/assert"

	"github.com/safing/portmaster/base/database"
)

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
