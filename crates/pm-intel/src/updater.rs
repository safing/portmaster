//! Update planning of the filter lists, port of the pure logic of Go
//! `service/intel/filterlists/updater.go` and `cache_version.go`.
//!
//! The file processing itself is in [`crate::FilterLists::load_list_file`];
//! this module decides which files must be applied, in which order, and tracks
//! the version of the cache database.

use pm_database::Interface;
use pm_record::{Key, Meta, Record};
use pm_updates::{Artifact, Version};

use crate::{CacheVersionRecord, Error, Result, CACHE_VERSION_KEY, RESET_VERSION};

/// Name of the base filter list file; contains all entries.
pub const BASE_LIST_FILE: &str = "base.dsdl";
/// Name of an intermediate filter list file.
pub const INTERMEDIATE_LIST_FILE: &str = "intermediate.dsdl";
/// Name of an urgent filter list file.
pub const URGENT_LIST_FILE: &str = "urgent.dsdl";
/// Name of the index file.
pub const LIST_INDEX_FILE: &str = "index.dsd";

/// All filter list files, in the order they are considered.
pub const LIST_FILES: &[&str] = &[BASE_LIST_FILE, INTERMEDIATE_LIST_FILE, URGENT_LIST_FILE];

/// Returns whether the file is the base filter list.
pub fn is_base_file(filename: &str) -> bool {
    filename == BASE_LIST_FILE
}

/// Reads the version the cache database was last updated to.
///
/// Returns `None` when the version is missing or was written with another
/// reset version, like Go `getCacheDatabaseVersion` returning `ErrNotFound`.
pub fn get_cache_database_version(cache: &Interface) -> Result<Option<Version>> {
    let record = match cache.get(CACHE_VERSION_KEY) {
        Ok(record) => record,
        Err(pm_database::Error::NotFound) => return Ok(None),
        Err(error) => return Err(error.into()),
    };
    let version: CacheVersionRecord = serde_json::from_slice(&record.data)?;
    if version.reset != RESET_VERSION {
        return Ok(None);
    }
    Version::parse(&version.version)
        .map(Some)
        .map_err(|error| Error::InvalidData(error.to_string()))
}

/// Returns the cache database version, or `v0.0.0` when unknown.
pub fn cache_database_version_or_zero(cache: &Interface) -> Version {
    get_cache_database_version(cache)
        .ok()
        .flatten()
        .unwrap_or_else(|| Version::parse("v0.0.0").expect("valid version"))
}

/// Stores the version the cache database was updated to.
pub fn set_cache_database_version(cache: &Interface, version: &str) -> Result<()> {
    let data = pm_dsd::gojson::to_vec(&CacheVersionRecord {
        version: version.to_string(),
        reset: RESET_VERSION.to_string(),
    })?;
    cache.put(Record {
        key: Key::parse(CACHE_VERSION_KEY),
        meta: Meta::default(),
        format: pm_dsd::format::JSON,
        data,
    })?;
    Ok(())
}

/// Sorts the artifacts by ascending version and returns the ones that must be
/// applied on top of `cache_version`, in processing order.
///
/// A base file newer than the cache version restarts processing: it already
/// contains all older changes, so earlier files are skipped.
pub fn resolve_update_order(
    mut update_order: Vec<Artifact>,
    cache_version: &Version,
) -> Vec<Artifact> {
    update_order.sort_by(|a, b| match (&a.version_num, &b.version_num) {
        (Some(a), Some(b)) => a.cmp(b),
        (Some(_), None) => std::cmp::Ordering::Greater,
        (None, Some(_)) => std::cmp::Ordering::Less,
        (None, None) => std::cmp::Ordering::Equal,
    });

    let mut start = None;
    for (index, artifact) in update_order.iter().enumerate() {
        let Some(version) = &artifact.version_num else {
            continue;
        };
        if version > cache_version && (start.is_none() || is_base_file(&artifact.filename)) {
            start = Some(index);
        }
    }

    match start {
        Some(start) => update_order.split_off(start),
        None => Vec::new(),
    }
}

/// Returns the artifacts without a valid semver, which Go skips with a warning.
pub fn without_valid_version(artifacts: Vec<Artifact>) -> (Vec<Artifact>, Vec<Artifact>) {
    artifacts
        .into_iter()
        .partition(|artifact| artifact.version_num.is_some())
}

/// Returns the highest version of the files that are about to be applied.
pub fn highest_version(artifacts: &[Artifact]) -> Option<&str> {
    artifacts
        .iter()
        .max_by(|a, b| match (&a.version_num, &b.version_num) {
            (Some(a), Some(b)) => a.cmp(b),
            (Some(_), None) => std::cmp::Ordering::Greater,
            (None, Some(_)) => std::cmp::Ordering::Less,
            (None, None) => std::cmp::Ordering::Equal,
        })
        .map(|artifact| artifact.version.as_str())
}
