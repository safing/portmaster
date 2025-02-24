package sqlite

// Base command for sql-migrate:
//go:generate -command migrate go tool github.com/rubenv/sql-migrate/sql-migrate

// Run missing migrations:
//go:generate migrate up --config=migrations_config.yml

// Redo last migration:
// x go:generate migrate redo --config=migrations_config.yml

// Undo all migrations:
// x go:generate migrate down --config=migrations_config.yml

// Generate models with bob:
//go:generate go tool github.com/stephenafamo/bob/gen/bobgen-sqlite

import (
	"embed"

	migrate "github.com/rubenv/sql-migrate"
	"github.com/safing/portmaster/base/database/record"
	"github.com/safing/portmaster/base/database/storage/sqlite/models"
)

//go:embed migrations/*
var dbMigrations embed.FS

func getMigrations() migrate.EmbedFileSystemMigrationSource {
	return migrate.EmbedFileSystemMigrationSource{
		FileSystem: dbMigrations,
		Root:       "migrations",
	}
}

func getMeta(r *models.Record) *record.Meta {
	meta := &record.Meta{
		Created:  r.Created,
		Modified: r.Modified,
		Expires:  r.Expires,
		Deleted:  r.Deleted,
	}
	if r.Secret {
		meta.MakeSecret()
	}
	if r.Crownjewel {
		meta.MakeCrownJewel()
	}
	return meta
}

func setMeta(r *models.Record, m *record.Meta) {
	r.Created = m.Created
	r.Modified = m.Modified
	r.Expires = m.Expires
	r.Deleted = m.Deleted
	r.Secret = m.IsSecret()
	r.Crownjewel = m.IsCrownJewel()
}
