//! Entity records, cache keys and the lookup API of the filter lists, a port of
//! Go `service/intel/filterlists/{record,keys,lookup,database}.go`.

use std::net::{IpAddr, Ipv4Addr, Ipv6Addr};
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::RwLock;

use pm_database::Interface;
use pm_record::{Key, Meta, Record};
use serde::{Deserialize, Serialize};

use crate::bloom::ScopedBloom;
use crate::decode::ListEntry;
use crate::{Error, Result};

/// Base path of filter list data in the cache database.
pub const CACHE_DB_PREFIX: &str = "cache:intel/filterlists";
/// Key of the filter lists cache version.
pub const CACHE_VERSION_KEY: &str = "cache:intel/filterlists/version";
/// Key of the filter lists index.
pub const INDEX_KEY: &str = "cache:intel/filterlists/index";
/// Prefix of the filter list entity records.
pub const LIST_KEY_PREFIX: &str = "cache:intel/filterlists/lists/";
/// Cache format version that forces a rebuild of the cache.
pub const RESET_VERSION: &str = "v0.6.0";

/// Returns the cache key of the bloom filter of a scope.
pub fn bloom_cache_key(scope: &str) -> String {
    format!("{CACHE_DB_PREFIX}/bloom/{scope}")
}

/// Returns the cache key of an entity record.
pub fn list_cache_key(scope: &str, key: &str) -> String {
    format!("{LIST_KEY_PREFIX}{scope}/{key}")
}

/// An entity record in the cache database, like Go `entityRecord`.
#[derive(Debug, Clone, Default, PartialEq, Eq, Serialize, Deserialize)]
pub struct EntityRecord {
    #[serde(rename = "Value")]
    pub value: String,
    #[serde(
        rename = "Sources",
        deserialize_with = "crate::deserialize_null_default"
    )]
    pub sources: Vec<String>,
    #[serde(rename = "Type")]
    pub type_: String,
    #[serde(rename = "UpdatedAt")]
    pub updated_at: i64,
}

/// A cached bloom filter, like Go `bloomFilterRecord`.
#[derive(Debug, Clone, Default, PartialEq, Eq, Serialize, Deserialize)]
pub struct BloomFilterRecord {
    #[serde(rename = "Filter")]
    pub filter: String,
}

/// A cached filter lists version, like Go `cacheVersionRecord`.
#[derive(Debug, Clone, Default, PartialEq, Eq, Serialize, Deserialize)]
pub struct CacheVersionRecord {
    #[serde(rename = "Version")]
    pub version: String,
    #[serde(rename = "Reset")]
    pub reset: String,
}

/// Normalizes an entry, like Go `normalizeEntry`.
///
/// Domains are lowercased and fully qualified (they end in a dot).
pub fn normalize_entry(entry: &mut ListEntry) {
    if entry.type_.eq_ignore_ascii_case("domain") {
        entry.entity = entry.entity.to_lowercase();
        if !entry.entity.ends_with('.') {
            entry.entity.push('.');
        }
    }
}

/// Builds the entity record of a filter list entry, like Go `processEntry`.
pub fn entity_record(entry: &ListEntry, updated_at: i64) -> Result<Record> {
    let mut meta = Meta::default();
    if entry.whitelist {
        meta.delete(updated_at);
    }

    let data = pm_dsd::gojson::to_vec(&EntityRecord {
        value: entry.entity.clone(),
        sources: entry.sources(),
        type_: entry.type_.clone(),
        updated_at,
    })?;

    Ok(Record {
        key: Key::parse(&list_cache_key(&entry.type_.to_lowercase(), &entry.entity)),
        meta,
        format: pm_dsd::format::JSON,
        data,
    })
}

/// The filter lists and their lookups.
pub struct FilterLists {
    cache: Interface,
    filter: RwLock<ScopedBloom>,
    loaded: AtomicBool,
}

impl FilterLists {
    /// Creates the filter lists on top of the given cache interface.
    pub fn new(cache: Interface) -> FilterLists {
        FilterLists {
            cache,
            filter: RwLock::new(ScopedBloom::new()),
            loaded: AtomicBool::new(false),
        }
    }

    /// Creates filter lists with small bloom filters, for tests.
    pub fn with_capacity(
        cache: Interface,
        domain: usize,
        asn: usize,
        country: usize,
        ipv4: usize,
        ipv6: usize,
    ) -> FilterLists {
        FilterLists {
            cache,
            filter: RwLock::new(ScopedBloom::with_sizes(domain, asn, country, ipv4, ipv6)),
            loaded: AtomicBool::new(false),
        }
    }

    /// Returns the cache interface.
    pub fn cache(&self) -> &Interface {
        &self.cache
    }

    /// Marks the filter lists as loaded or not loaded.
    pub fn set_loaded(&self, loaded: bool) {
        self.loaded.store(loaded, Ordering::SeqCst);
    }

    /// Returns whether the filter lists were loaded.
    pub fn is_loaded(&self) -> bool {
        self.loaded.load(Ordering::SeqCst)
    }

    /// Returns the capacity report of the bloom filters.
    pub fn capacity_report(&self) -> String {
        self.filter.read().expect("lock poisoned").capacity_report()
    }

    /// Normalizes and stores a filter list entry, like Go `processEntry`.
    ///
    /// Entries with sources are added to the bloom filter; whitelist entries are
    /// stored as deleted records.
    pub fn store_entry(&self, entry: &ListEntry, updated_at: i64) -> Result<Record> {
        let mut entry = entry.clone();
        normalize_entry(&mut entry);

        if !entry.resources.is_empty() {
            self.filter
                .write()
                .expect("lock poisoned")
                .add(&entry.type_, &entry.entity);
        }

        let record = entity_record(&entry, updated_at)?;
        self.cache.put(record.clone())?;
        Ok(record)
    }

    /// Tests the bloom filter of an entity type, like Go's `defaultFilter.test`.
    ///
    /// Unknown entity types simulate a match.
    pub fn bloom_test(&self, entity_type: &str, value: &str) -> bool {
        self.filter
            .read()
            .expect("lock poisoned")
            .test(entity_type, value)
    }

    /// Looks up the sources that contain the entity, like Go `lookupBlockLists`.
    pub fn lookup(&self, entity_type: &str, value: &str) -> Result<Vec<String>> {
        let key = list_cache_key(entity_type, value);
        if !self.is_loaded() {
            tracing::warn!(key, "intel/filterlists: filter lists are not loaded");
            return Ok(Vec::new());
        }

        if !self
            .filter
            .read()
            .expect("lock poisoned")
            .test(entity_type, value)
        {
            return Ok(Vec::new());
        }

        match self.cache.get(&key) {
            Ok(record) => {
                let entity: EntityRecord = serde_json::from_slice(&record.data)?;
                Ok(entity.sources)
            }
            Err(pm_database::Error::NotFound) => Ok(Vec::new()),
            Err(error) => {
                tracing::error!(key, %error, "intel/filterlists: failed to get entries");
                Err(Error::Database(error))
            }
        }
    }

    /// Looks up the sources that block the country.
    pub fn lookup_country(&self, country: &str) -> Result<Vec<String>> {
        self.lookup("country", country)
    }

    /// Looks up the sources that block the domain.
    ///
    /// Empty domains and the root zone are never listed.
    pub fn lookup_domain(&self, domain: &str) -> Result<Vec<String>> {
        match domain {
            "" | "." => Ok(Vec::new()),
            _ => self.lookup("domain", domain),
        }
    }

    /// Looks up the sources that block the ASN.
    pub fn lookup_asn(&self, asn: &str) -> Result<Vec<String>> {
        self.lookup("asn", asn)
    }

    /// Looks up the sources that block the IPv4 address.
    pub fn lookup_ipv4(&self, ipv4: Ipv4Addr) -> Result<Vec<String>> {
        self.lookup("ipv4", &ipv4.to_string())
    }

    /// Looks up the sources that block the IPv6 address.
    pub fn lookup_ipv6(&self, ipv6: Ipv6Addr) -> Result<Vec<String>> {
        self.lookup("ipv6", &ipv6.to_string())
    }

    /// Looks up the sources that block the IP address, like Go `LookupIP`.
    pub fn lookup_ip(&self, ip: IpAddr) -> Result<Vec<String>> {
        match ip {
            IpAddr::V4(ipv4) => self.lookup_ipv4(ipv4),
            IpAddr::V6(ipv6) => self.lookup_ipv6(ipv6),
        }
    }

    /// Saves the bloom filters to the cache database, like Go `saveToCache`.
    pub fn save_bloom_cache(&self) -> Result<()> {
        let filter = self.filter.read().expect("lock poisoned");
        for (scope, ring) in filter.rings() {
            let record = BloomFilterRecord {
                filter: hex::encode(ring.marshal_binary()),
            };
            let data = pm_dsd::gojson::to_vec(&record)?;
            self.cache.put(Record {
                key: Key::parse(&bloom_cache_key(scope)),
                meta: Meta::default(),
                format: pm_dsd::format::JSON,
                data,
            })?;
        }
        Ok(())
    }

    /// Loads the bloom filters from the cache database, like Go
    /// `loadFromCache`.
    pub fn load_bloom_cache(&self) -> Result<()> {
        let mut loaded = Vec::with_capacity(ScopedBloom::scopes().len());
        for scope in ScopedBloom::scopes() {
            let record = self.cache.get(&bloom_cache_key(scope))?;
            let filter: BloomFilterRecord = serde_json::from_slice(&record.data)?;
            let blob = hex::decode(&filter.filter)
                .map_err(|error| Error::InvalidData(error.to_string()))?;
            loaded.push((*scope, crate::Ring::unmarshal_binary(&blob)?));
        }

        let mut filter = self.filter.write().expect("lock poisoned");
        for (scope, ring) in loaded {
            filter.set_ring(scope, ring);
        }
        Ok(())
    }

    /// Returns the cache version, like Go `getCacheDatabaseVersion`.
    ///
    /// Returns `None` when the cache version is missing, unreadable or was
    /// written with a different reset version.
    pub fn cache_version(&self) -> Result<Option<CacheVersionRecord>> {
        let record = match self.cache.get(CACHE_VERSION_KEY) {
            Ok(record) => record,
            Err(pm_database::Error::NotFound) => return Ok(None),
            Err(error) => return Err(Error::Database(error)),
        };
        let version: CacheVersionRecord = serde_json::from_slice(&record.data)?;
        if version.reset != RESET_VERSION {
            return Ok(None);
        }
        Ok(Some(version))
    }

    /// Sets the cache version, like Go `setCacheDatabaseVersion`.
    pub fn set_cache_version(&self, version: &str) -> Result<()> {
        let data = pm_dsd::gojson::to_vec(&CacheVersionRecord {
            version: version.to_string(),
            reset: RESET_VERSION.to_string(),
        })?;
        self.cache.put(Record {
            key: Key::parse(CACHE_VERSION_KEY),
            meta: Meta::default(),
            format: pm_dsd::format::JSON,
            data,
        })?;
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::decode::EntryResource;

    #[test]
    fn keys_match_go() {
        assert_eq!(
            bloom_cache_key("domain"),
            "cache:intel/filterlists/bloom/domain"
        );
        assert_eq!(
            list_cache_key("domain", "example.com."),
            "cache:intel/filterlists/lists/domain/example.com."
        );
    }

    #[test]
    fn domains_are_normalized() {
        let mut entry = ListEntry {
            type_: "domain".to_string(),
            entity: "Example.COM".to_string(),
            ..ListEntry::default()
        };
        normalize_entry(&mut entry);
        assert_eq!(entry.entity, "example.com.");

        let mut entry = ListEntry {
            type_: "domain".to_string(),
            entity: "example.com.".to_string(),
            ..ListEntry::default()
        };
        normalize_entry(&mut entry);
        assert_eq!(entry.entity, "example.com.");

        // Other types and empty domains are left alone.
        let mut entry = ListEntry {
            type_: "ipv4".to_string(),
            entity: "192.0.2.10".to_string(),
            ..ListEntry::default()
        };
        normalize_entry(&mut entry);
        assert_eq!(entry.entity, "192.0.2.10");

        let mut entry = ListEntry {
            type_: "domain".to_string(),
            entity: String::new(),
            ..ListEntry::default()
        };
        normalize_entry(&mut entry);
        assert_eq!(entry.entity, ".");
    }

    #[test]
    fn records_carry_the_normalized_values() {
        let entry = ListEntry {
            type_: "domain".to_string(),
            entity: "example.com.".to_string(),
            whitelist: false,
            resources: vec![
                EntryResource {
                    source_id: "list-a".to_string(),
                    resource_id: "r1".to_string(),
                },
                EntryResource {
                    source_id: "list-a".to_string(),
                    resource_id: "r2".to_string(),
                },
            ],
        };
        let record = entity_record(&entry, 1_700_000_000).unwrap();
        assert_eq!(
            record.key.to_string(),
            "cache:intel/filterlists/lists/domain/example.com."
        );
        assert!(!record.meta.is_deleted());
        let decoded: EntityRecord = serde_json::from_slice(&record.data).unwrap();
        assert_eq!(decoded.value, "example.com.");
        assert_eq!(decoded.sources, vec!["list-a"]);
        assert_eq!(decoded.type_, "domain");
        assert_eq!(decoded.updated_at, 1_700_000_000);

        // Whitelist entries are stored as deleted records.
        let entry = ListEntry {
            whitelist: true,
            ..entry
        };
        let record = entity_record(&entry, 1_700_000_000).unwrap();
        assert!(record.meta.is_deleted());
    }
}
