//! Compatible implementation of Go `service/intel/filterlists`.
//!
//! The crate matches entities (domains, IPs, ASNs, countries) against cached
//! filter lists: a scoped bloom filter answers the "definitely not listed"
//! question, and the entity record in the `cache` database lists the sources
//! that contain the entity.
//!
//! # Compatibility notes
//!
//! * The bloom filters are byte-compatible with `github.com/tannerryan/ring`
//!   (MurmurHash3 x64 128, four-hash scheme, serialization with version 1), so
//!   filters cached by the Go core can be loaded.
//! * The DSDL decoder only supports JSON entries; the Go code accepts any DSD
//!   format with a complex structure. Filterlist files use JSON.
//! * The filterlists index, the updater, `service/updates` integration and the
//!   module lifecycle are not ported yet. [`FilterLists::store_entry`] writes
//!   records one by one instead of batching by 1000.
//! * The database interface is passed in ([`FilterLists::new`]); Go uses a
//!   package-level `cache` interface with local and internal permissions.
//! * Bloom filters loaded from the cache start with a zero add counter, like in
//!   Go.

mod block_reason;
mod bloom;
pub mod customlists;
mod decode;
mod entity;
mod index;
mod module;
mod record;
mod updater;

use pm_database::Interface;

pub use block_reason::{ListBlockReason, ListMatch};
pub use bloom::{
    BloomError, Ring, ScopedBloom, ASN_BF_SIZE, BF_FALSE_POSITIVE_RATE, COUNTRY_BF_SIZE,
    DOMAIN_BF_SIZE, IPV4_BF_SIZE, IPV6_BF_SIZE,
};
pub use decode::{decode_file, parse_header, EntryResource, ListEntry};
pub use entity::{split_domain, Entity, LocationProvider, ReverseResolver, StaticLocations};
pub use module::{
    FilterListsModule, FILTERLISTS_DISABLED_STATE, FILTERLISTS_STALE_DATA_STATE,
    FILTERLISTS_UPDATE_FAILED_STATE,
};
pub use index::{
    are_semvers_equal, resolve_list_ids, unbreak_filter_list_ids, update_list_index, Category,
    ListIndexFile, Source, UNBREAK_CATEGORY_IDS,
};
pub use updater::{
    cache_database_version_or_zero, get_cache_database_version, highest_version, is_base_file,
    resolve_update_order, set_cache_database_version, update_filter_lists, without_valid_version,
    FilterListUpdate, BASE_LIST_FILE,
    INTERMEDIATE_LIST_FILE, LIST_FILES, LIST_INDEX_FILE, URGENT_LIST_FILE,
};
pub use record::{
    bloom_cache_key, AppliedUpdate, list_cache_key, normalize_entry, BloomFilterRecord, CacheVersionRecord,
    EntityRecord, FilterLists, CACHE_DB_PREFIX, CACHE_VERSION_KEY, INDEX_KEY, LIST_KEY_PREFIX,
    RESET_VERSION,
};

/// Errors of the intel filter lists.
#[derive(Debug, thiserror::Error)]
pub enum Error {
    #[error("database: {0}")]
    Database(#[from] pm_database::Error),
    #[error("io: {0}")]
    Io(#[from] std::io::Error),
    #[error("json: {0}")]
    Json(#[from] serde_json::Error),
    #[error("dsd: {0}")]
    Dsd(#[from] pm_dsd::Error),
    #[error("invalid data: {0}")]
    InvalidData(String),
    #[error("{0}")]
    Decode(String),
    #[error("{0}")]
    Message(String),
    #[error("bloom filter: {0}")]
    Bloom(#[from] BloomError),
}

/// Deserializes `null` as the default value, like Go decoding `nil` into a
/// slice.
pub(crate) fn deserialize_null_default<'de, D, T>(
    deserializer: D,
) -> std::result::Result<T, D::Error>
where
    D: serde::Deserializer<'de>,
    T: Default + serde::Deserialize<'de>,
{
    use serde::Deserialize;
    Ok(Option::<T>::deserialize(deserializer)
        .map_err(serde::de::Error::custom)?
        .unwrap_or_default())
}

/// Result type of the intel filter lists.
pub type Result<T> = std::result::Result<T, Error>;

/// Creates the cache interface used by the filter lists, like Go's
/// package-level interface (local and internal permissions).
pub fn cache_interface(databases: &std::sync::Arc<pm_database::Databases>) -> Interface {
    databases.interface(pm_database::Options {
        local: true,
        internal: true,
        ..pm_database::Options::default()
    })
}
