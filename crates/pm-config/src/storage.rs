//! Database storage that exposes the option registry as the `config` database.

use std::sync::Arc;

use pm_database::{Error as DbError, Query, Record, Storage};
use serde_json::Value;

use crate::option::{OPT_TYPE_BOOL, OPT_TYPE_INT, OPT_TYPE_STRING, OPT_TYPE_STRING_ARRAY};
use crate::{Config, Error};

/// Injected storage for the `config` database.
pub struct ConfigStorage {
    config: Arc<Config>,
}

impl ConfigStorage {
    pub fn new(config: Arc<Config>) -> ConfigStorage {
        ConfigStorage { config }
    }

    fn export_record(&self, key: &str) -> pm_database::Result<Record> {
        self.config.export_record(key).map_err(map_error)
    }
}

fn map_error(error: Error) -> DbError {
    match error {
        Error::UnknownOption(_) => DbError::StorageNotFound,
        other => DbError::Other(other.to_string()),
    }
}

fn typed_value(config: &Config, key: &str, record: &Record) -> pm_database::Result<Value> {
    let Some(accessor) = record.accessor() else {
        return Err(DbError::Other("invalid data".to_string()));
    };

    let option = config.option(key).map_err(map_error)?;
    let value = match option.definition().opt_type {
        OPT_TYPE_STRING => accessor.get_string("Value").map(Value::String),
        OPT_TYPE_STRING_ARRAY => accessor
            .get_string_array("Value")
            .map(|values| Value::Array(values.into_iter().map(Value::String).collect())),
        OPT_TYPE_INT => accessor.get_int("Value").map(Value::from),
        OPT_TYPE_BOOL => accessor.get_bool("Value").map(Value::Bool),
        _ => None,
    };

    value.ok_or_else(|| DbError::Other("received invalid value in \"Value\"".to_string()))
}

impl Storage for ConfigStorage {
    fn get(&self, key: &str) -> pm_database::Result<Record> {
        self.export_record(key)
    }

    fn put(&self, record: Record) -> pm_database::Result<Record> {
        let key = record.key.key.clone();

        if record.meta.is_deleted() {
            self.config.unset_config_option(&key).map_err(map_error)?;
            return self.export_record(&key);
        }

        let value = record.accessor().and_then(|accessor| accessor.get("Value"));
        match value {
            Some(value) if !value.is_null() => {
                let value = typed_value(&self.config, &key, &record)?;
                self.config
                    .set_config_option(&key, value)
                    .map_err(map_error)?;
            }
            _ => {
                self.config.unset_config_option(&key).map_err(map_error)?;
            }
        }

        self.export_record(&key)
    }

    fn delete(&self, key: &str) -> pm_database::Result<()> {
        self.config.unset_config_option(key).map_err(map_error)
    }

    fn query(
        &self,
        query: &Query,
        _local: bool,
        _internal: bool,
    ) -> pm_database::Result<Vec<Record>> {
        let prefix = query.database_key_prefix();
        let mut records = Vec::new();
        // Options are kept sorted by key.
        for option in self.config.options_snapshot() {
            if !option.key().starts_with(&prefix) {
                continue;
            }
            records.push(self.export_record(option.key())?);
        }
        Ok(records)
    }

    fn read_only(&self) -> bool {
        false
    }

    fn injected(&self) -> bool {
        true
    }
}
