//! Value providers of the runtime registry.

use std::sync::Arc;
use std::time::Instant;

use pm_record::Record;

use crate::registry::{Error, Result};

/// Provides access to a runtime-computed database record.
pub trait ValueProvider: Send + Sync {
    /// Returns one or more records that match the key or prefix.
    fn get(&self, key_or_prefix: &str) -> Result<Vec<Record>>;

    /// Sets a value from outside.
    ///
    /// Read-only providers return [`Error::ReadOnly`].
    fn set(&self, record: Record) -> Result<Option<Record>>;
}

/// Getter function of a [`SimpleValueGetter`].
type Getter = Box<dyn Fn(&str) -> Result<Vec<Record>> + Send + Sync + 'static>;
/// Setter function of a [`SimpleValueSetter`].
type Setter = Box<dyn Fn(Record) -> Result<Option<Record>> + Send + Sync + 'static>;

/// A convenience provider for read-only single values, like Go
/// `SimpleValueGetterFunc`.
pub struct SimpleValueGetter(Getter);

impl SimpleValueGetter {
    /// Creates a read-only provider from a getter function.
    pub fn new<F>(get: F) -> SimpleValueGetter
    where
        F: Fn(&str) -> Result<Vec<Record>> + Send + Sync + 'static,
    {
        SimpleValueGetter(Box::new(get))
    }
}

impl ValueProvider for SimpleValueGetter {
    fn get(&self, key_or_prefix: &str) -> Result<Vec<Record>> {
        (self.0)(key_or_prefix)
    }

    fn set(&self, _record: Record) -> Result<Option<Record>> {
        Err(Error::ReadOnly)
    }
}

/// A convenience provider for write-only values, like Go
/// `SimpleValueSetterFunc`.
pub struct SimpleValueSetter(Setter);

impl SimpleValueSetter {
    /// Creates a write-only provider from a setter function.
    pub fn new<F>(set: F) -> SimpleValueSetter
    where
        F: Fn(Record) -> Result<Option<Record>> + Send + Sync + 'static,
    {
        SimpleValueSetter(Box::new(set))
    }
}

impl ValueProvider for SimpleValueSetter {
    fn get(&self, _key_or_prefix: &str) -> Result<Vec<Record>> {
        Err(Error::WriteOnly)
    }

    fn set(&self, record: Record) -> Result<Option<Record>> {
        (self.0)(record)
    }
}

/// Read-only access to a single record, like Go `ProvideRecord`.
///
/// The record is only returned when the requested key matches its key exactly.
#[derive(Clone)]
pub struct SingleRecord {
    record: Record,
}

impl SingleRecord {
    /// Wraps the record into a read-only provider.
    pub fn new(record: Record) -> SingleRecord {
        SingleRecord { record }
    }

    /// Returns the wrapped record.
    pub fn record(&self) -> &Record {
        &self.record
    }

    /// Allows replacing the wrapped record, like re-pushing a computed value.
    pub fn replace(&mut self, record: Record) {
        self.record = record;
    }
}

impl ValueProvider for SingleRecord {
    fn get(&self, key_or_prefix: &str) -> Result<Vec<Record>> {
        if key_or_prefix != self.record.key.key {
            return Ok(Vec::new());
        }
        Ok(vec![self.record.clone()])
    }

    fn set(&self, _record: Record) -> Result<Option<Record>> {
        Err(Error::ReadOnly)
    }
}

/// Wraps a provider into a read-only [`ValueProvider`], like Go
/// `ProvideRecord`.
pub fn provide_record(record: Record) -> Arc<dyn ValueProvider> {
    Arc::new(SingleRecord::new(record))
}

/// Wraps a provider and traces all calls to `get` and `set`, like Go
/// `TraceProvider`.
pub struct TraceProvider {
    provider: Arc<dyn ValueProvider>,
}

impl TraceProvider {
    /// Wraps the given provider.
    pub fn new(provider: Arc<dyn ValueProvider>) -> TraceProvider {
        TraceProvider { provider }
    }
}

impl ValueProvider for TraceProvider {
    fn get(&self, key_or_prefix: &str) -> Result<Vec<Record>> {
        let started = Instant::now();
        let result = self.provider.get(key_or_prefix);
        tracing::trace!(
            key_or_prefix,
            duration = ?started.elapsed(),
            error = ?result.as_ref().err(),
            records = result.as_ref().map(Vec::len).unwrap_or(0),
            "runtime: loading records"
        );
        result
    }

    fn set(&self, record: Record) -> Result<Option<Record>> {
        let key = record.key.key.clone();
        let started = Instant::now();
        let result = self.provider.set(record);
        tracing::trace!(
            key,
            duration = ?started.elapsed(),
            error = ?result.as_ref().err(),
            "runtime: setting record"
        );
        result
    }
}
