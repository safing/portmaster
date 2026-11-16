//! Port of Go `base/database/migration`: a registry of data migrations that
//! are applied in semver order, with the last successful version persisted in
//! a database record (as a raw DSD payload).
//!
//! Unlike Go, migrations receive a `pm_database::Interface` directly instead
//! of creating a new one from global state.

use pm_database::{Error as DatabaseError, Interface, Options};
use pm_record::{Key, Meta, Record};
use pm_updates::Version;
use std::fmt;

/// One planned migration step, used in diagnostics.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct DiagnosticStep {
    pub version: String,
    pub description: String,
}

/// Detailed report of a migration run, like Go `migration.Diagnostics`.
#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct Diagnostics {
    pub message: String,
    pub wrapped: String,
    pub start_of_migration: String,
    pub last_successful_migration: String,
    pub target_version: String,
    pub execution_plan: Vec<DiagnosticStep>,
    pub failed_migration: String,
}

impl fmt::Display for Diagnostics {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        if !self.failed_migration.is_empty() {
            write!(f, "{}: ", self.failed_migration)?;
        }
        if !self.message.is_empty() {
            write!(f, "{}: ", self.message)?;
        }
        f.write_str(&self.wrapped)
    }
}

impl std::error::Error for Diagnostics {}

/// Errors of the migration registry.
#[derive(Debug, thiserror::Error)]
pub enum Error {
    #[error("migration {description}: invalid version {version}: {source}")]
    InvalidVersion {
        description: String,
        version: String,
        source: pm_updates::VersionError,
    },
    #[error("{0}")]
    Database(#[from] DatabaseError),
    #[error("{0}")]
    Failed(Box<Diagnostics>),
}

/// A migration callback: `(from, to)` versions and the database interface.
pub type MigrateFunc = dyn Fn(&Interface, &Version, &Version) -> Result<(), String> + Send + Sync;

/// A registered data migration.
pub struct Migration {
    pub description: String,
    pub version: Version,
    pub migrate: Box<MigrateFunc>,
}

impl Migration {
    pub fn new(
        description: impl Into<String>,
        version: &str,
        migrate: impl Fn(&Interface, &Version, &Version) -> Result<(), String> + Send + Sync + 'static,
    ) -> Result<Migration, Error> {
        let description = description.into();
        Ok(Migration {
            version: Version::parse(version).map_err(|source| Error::InvalidVersion {
                description: description.clone(),
                version: version.to_string(),
                source,
            })?,
            description,
            migrate: Box::new(migrate),
        })
    }
}

/// A stack of migrations for one database key.
pub struct Registry {
    key: String,
    migrations: Vec<Migration>,
}

impl Registry {
    /// Creates a registry. `key` is the database key storing the last
    /// successfully applied version, e.g. `core:migrations/profile`.
    pub fn new(key: impl Into<String>) -> Registry {
        Registry {
            key: key.into(),
            migrations: Vec::new(),
        }
    }

    /// Registered migrations in insertion order.
    pub fn migrations(&self) -> &[Migration] {
        &self.migrations
    }

    /// Adds migrations to the stack.
    pub fn add(&mut self, migrations: Vec<Migration>) -> Result<(), Error> {
        self.migrations.extend(migrations);
        Ok(())
    }

    /// Applies all pending migrations in increasing version order.
    pub fn migrate(&self, db: &Interface) -> Result<Diagnostics, Error> {
        let mut diag = Diagnostics::default();
        let start = self.latest_successful(db)?;
        if let Some(version) = &start {
            diag.start_of_migration = version.to_string();
        }

        let mut plan = self.migrations.iter().collect::<Vec<_>>();
        plan.sort_by(|a, b| a.version.cmp(&b.version));
        let plan = plan
            .into_iter()
            .filter(|migration| {
                start
                    .as_ref()
                    .map(|start| migration.version > *start)
                    .unwrap_or(true)
            })
            .collect::<Vec<_>>();
        for migration in &plan {
            diag.execution_plan.push(DiagnosticStep {
                version: migration.version.to_string(),
                description: migration.description.clone(),
            });
        }
        if plan.is_empty() {
            return Ok(diag);
        }
        diag.target_version = plan.last().unwrap().version.to_string();

        let mut last = start;
        for migration in plan {
            let from = last.clone().unwrap_or_else(|| Version::parse("0.0.0").unwrap());
            if let Err(message) = (migration.migrate)(db, &from, &migration.version) {
                diag.failed_migration = migration.description.clone();
                diag.wrapped = message;
                return Err(Error::Failed(Box::new(diag)));
            }
            last = Some(migration.version.clone());
            diag.last_successful_migration = migration.version.to_string();
            self.save_last_successful(db, &migration.version)?;
        }
        Ok(diag)
    }

    fn latest_successful(&self, db: &Interface) -> Result<Option<Version>, Error> {
        let record = match db.get(&self.key) {
            Ok(record) => record,
            Err(DatabaseError::NotFound) => return Ok(None),
            Err(error) => {
                return Err(Error::Failed(Box::new(Diagnostics {
                    message: "failed to query database for migration status".to_string(),
                    wrapped: error.to_string(),
                    ..Diagnostics::default()
                })));
            }
        };
        let text = String::from_utf8_lossy(&record.data);
        Version::parse(text.trim())
            .map(Some)
            .map_err(|_| Error::Failed(Box::new(Diagnostics {
                message: "failed to parse version stored in migration status record".to_string(),
                wrapped: text.to_string(),
                ..Diagnostics::default()
            })))
    }

    fn save_last_successful(&self, db: &Interface, version: &Version) -> Result<(), Error> {
        db.put(Record {
            key: Key::parse(&self.key),
            meta: Meta::default(),
            format: pm_dsd::format::RAW,
            data: version.to_string().into_bytes(),
        })?;
        Ok(())
    }
}

/// Returns an interface with local and internal permissions, like Go's
/// `database.NewInterface(&Options{Local: true, Internal: true})`.
pub fn internal_interface(databases: &std::sync::Arc<pm_database::Databases>) -> Interface {
    databases.interface(Options {
        local: true,
        internal: true,
        ..Options::default()
    })
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::sync::atomic::{AtomicUsize, Ordering};
    use std::sync::Arc;

    fn migration(description: &str, version: &str, calls: Arc<AtomicUsize>) -> Result<Migration, Error> {
        Migration::new(description, version, move |_, _, _| {
            calls.fetch_add(1, Ordering::SeqCst);
            Ok(())
        })
    }

    #[test]
    fn rejects_invalid_versions() {
        assert!(Migration::new("bad", "not-a-version", |_, _, _| Ok(())).is_err());
    }

    #[test]
    fn registry_is_injected_and_versions_are_normalized() {
        let mut registry = Registry::new("core:migrations/test");
        let calls = Arc::new(AtomicUsize::new(0));
        registry
            .add(vec![
                migration("first", "v1.4.7", Arc::clone(&calls)).unwrap(),
                migration("second", "1.6.3", Arc::clone(&calls)).unwrap(),
            ])
            .unwrap();
        assert_eq!(registry.migrations[0].version.to_string(), "1.4.7");
        assert_eq!(registry.migrations[1].version.to_string(), "1.6.3");
    }

    #[test]
    fn diagnostics_format_matches_go() {
        let diag = Diagnostics {
            failed_migration: "Migrate icons".to_string(),
            message: "failed to save".to_string(),
            wrapped: "db error".to_string(),
            ..Diagnostics::default()
        };
        assert_eq!(diag.to_string(), "Migrate icons: failed to save: db error");
    }
}
