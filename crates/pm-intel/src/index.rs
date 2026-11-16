//! Filter list index, port of Go `service/intel/filterlists/index.go`.
//!
//! The index describes the released filter list sources and their categories.
//! It is stored in the cache database and used to resolve configured source or
//! category IDs into distinct source IDs.

use std::path::Path;

use pm_database::Interface;
use pm_record::{Key, Meta, Record};
use serde::{Deserialize, Serialize};

use crate::{Result, INDEX_KEY};

/// A category of filter list sources.
#[derive(Debug, Clone, Default, PartialEq, Eq, Serialize, Deserialize)]
pub struct Category {
    /// Unique category ID.
    #[serde(rename = "id")]
    pub id: String,
    /// ID of the parent category, if any.
    #[serde(rename = "parent", default, skip_serializing_if = "String::is_empty")]
    pub parent: String,
    /// Human readable name.
    #[serde(rename = "name")]
    pub name: String,
    /// Human readable description.
    #[serde(
        rename = "description",
        default,
        skip_serializing_if = "String::is_empty"
    )]
    pub description: String,
}

/// An external filter list source.
#[derive(Debug, Clone, Default, PartialEq, Eq, Serialize, Deserialize)]
pub struct Source {
    /// Unique source ID, referenced by list entries.
    #[serde(rename = "id")]
    pub id: String,
    /// Human readable name.
    #[serde(rename = "name")]
    pub name: String,
    /// Human readable description.
    #[serde(rename = "description", default)]
    pub description: String,
    /// Type of entities the source provides.
    #[serde(rename = "type")]
    pub type_: String,
    /// URL of the filter list file.
    #[serde(rename = "url")]
    pub url: String,
    /// ID of the category the source belongs to.
    #[serde(rename = "category")]
    pub category: String,
    /// Website of the source maintainers.
    #[serde(rename = "website", default, skip_serializing_if = "String::is_empty")]
    pub website: String,
    /// License of the source.
    #[serde(rename = "license", default)]
    pub license: String,
    /// How to contribute to the source.
    #[serde(rename = "contribute", default)]
    pub contribute: String,
}

/// The released filter list index.
#[derive(Debug, Clone, Default, PartialEq, Eq, Serialize, Deserialize)]
pub struct ListIndexFile {
    /// Version of the index.
    #[serde(rename = "version", default)]
    pub version: String,
    /// Schema version of the index.
    #[serde(rename = "schemaVersion", default)]
    pub schema_version: String,
    /// Known categories.
    #[serde(rename = "categories", default)]
    pub categories: Vec<Category>,
    /// Known sources.
    #[serde(rename = "sources", default)]
    pub sources: Vec<Source>,
}

impl ListIndexFile {
    /// Parses an index in any DSD format (the released file is DSD).
    pub fn from_bytes(data: &[u8]) -> Result<ListIndexFile> {
        let (index, _) = pm_dsd::load::<ListIndexFile>(data)?;
        Ok(index)
    }

    /// Returns the IDs of all sources of a category and its child categories.
    pub fn category_sources(&self, id: &str) -> Vec<String> {
        let mut ids = std::collections::BTreeSet::new();
        self.collect_category_sources(id, &mut ids, &mut Vec::new());
        ids.into_iter().collect()
    }

    fn collect_category_sources(
        &self,
        id: &str,
        ids: &mut std::collections::BTreeSet<String>,
        visited: &mut Vec<String>,
    ) {
        if visited.iter().any(|seen| seen == id) {
            // A broken index may contain a category cycle.
            return;
        }
        visited.push(id.to_string());

        for source in &self.sources {
            if source.category == id {
                ids.insert(source.id.clone());
            }
        }
        for category in &self.categories {
            if category.parent == id {
                self.collect_category_sources(&category.id, ids, visited);
            }
        }
    }

    /// Resolves a source ID or a category ID to source IDs.
    pub fn sources_matching(&self, id: &str) -> Vec<String> {
        if let Some(source) = self.sources.iter().find(|source| source.id == id) {
            return vec![source.id.clone()];
        }
        self.category_sources(id)
    }

    /// Resolves a list of source or category IDs to distinct source IDs.
    pub fn distinct_source_ids(&self, ids: &[String]) -> Vec<String> {
        let mut distinct = std::collections::BTreeSet::new();
        for id in ids {
            for source_id in self.sources_matching(id) {
                distinct.insert(source_id);
            }
        }
        distinct.into_iter().collect()
    }

    /// Stores the index in the cache database.
    pub fn store(&self, cache: &Interface) -> Result<()> {
        let data = pm_dsd::gojson::to_vec(self)?;
        cache.put(Record {
            key: Key::parse(INDEX_KEY),
            meta: Meta::default(),
            format: pm_dsd::format::JSON,
            data,
        })?;
        Ok(())
    }

    /// Loads the index from the cache database, if present.
    pub fn load(cache: &Interface) -> Result<Option<ListIndexFile>> {
        match cache.get(INDEX_KEY) {
            Ok(record) => Ok(Some(serde_json::from_slice(&record.data)?)),
            Err(pm_database::Error::NotFound) => Ok(None),
            Err(error) => Err(error.into()),
        }
    }
}

/// Compares two version strings; both must parse and be equal.
pub fn are_semvers_equal(first: &str, second: &str) -> bool {
    let Ok(first) = pm_updates::Version::parse(first) else {
        return false;
    };
    let Ok(second) = pm_updates::Version::parse(second) else {
        return false;
    };
    first == second
}

/// Updates the cached index from `index_file`, if the version differs.
///
/// Returns whether the cache was updated. `version` is the version of the
/// update that provides the index file, like the artifact version in Go.
pub fn update_list_index(
    cache: &Interface,
    index_file: impl AsRef<Path>,
    version: &str,
) -> Result<bool> {
    let current = ListIndexFile::load(cache)?;
    if let Some(current) = &current {
        let current_version = current.version.trim_start_matches('v');
        if current_version == version || are_semvers_equal(version, &current.version) {
            tracing::debug!(
                "filterlists: index is up to date ({} == {version})",
                current.version
            );
            return Ok(false);
        }
        tracing::info!(
            "filterlists: index from cache is outdated, starting update ({current_version} != {version})"
        );
    } else {
        tracing::info!("filterlists: index not in cache, starting update");
    }

    let blob = std::fs::read(index_file)?;
    let index = ListIndexFile::from_bytes(&blob)?;
    index.store(cache)?;
    tracing::debug!(
        "intel/filterlists: updated list index in cache to {}",
        index.version
    );
    Ok(true)
}

/// Resolves source or category IDs through the cached index.
///
/// Returns `Ok(None)` when the index is not cached yet; the caller updates it
/// from the update system and retries, like Go `ResolveListIDs`.
pub fn resolve_list_ids(cache: &Interface, ids: &[String]) -> Result<Option<Vec<String>>> {
    let Some(index) = ListIndexFile::load(cache)? else {
        return Ok(None);
    };
    let resolved = index.distinct_source_ids(ids);
    tracing::debug!("intel/filterlists: resolved ids {ids:?} to {resolved:?}");
    Ok(Some(resolved))
}

/// Category used for filter lists that must not be blocked by other lists.
pub const UNBREAK_CATEGORY_IDS: &[&str] = &["UNBREAK"];

/// Resolves the unbreak filter list IDs, like Go
/// `UpdateUnbreakFilterListIDs`.
pub fn unbreak_filter_list_ids(cache: &Interface) -> Result<Option<Vec<String>>> {
    let ids: Vec<String> = UNBREAK_CATEGORY_IDS
        .iter()
        .map(|id| (*id).to_string())
        .collect();
    resolve_list_ids(cache, &ids)
}
