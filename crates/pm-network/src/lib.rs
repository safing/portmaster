//! Port of the Go `service/network` package: the connection model, the
//! connection stores, the `network:` database, the cleaner and DNS request
//! bookkeeping.
//!
//! # Compatibility notes
//!
//! * Connections serialize with Go field names and order. Go writes nil
//!   slices and maps as `null`; Rust cannot tell nil from empty, so empty
//!   entity lists are written as `null` unless noted below.
//! * `BlockedByLists`/`BlockedEntities` are `[]` after `Entity::match_lists`
//!   (Go `makeDistinct` returns non-nil) and `null` before; `CNAME` and
//!   `ListOccurences` cannot be distinguished and stay `null` when empty.
//! * `BandwidthEnabled` is always `true`, deliberately following the user's
//!   local Go change and not upstream (`false`).
//! * `TunnelContext` (SPN) is not ported and always `null`.
//! * `DNSContext.Modified`/`Expires` are RFC 3339 strings, like Go's
//!   `time.Time` JSON.
//! * `PacketInfo` is the crate's own port of `packet.Info`: the raw protocol
//!   number is needed for IDs, and addresses may be missing (DNS requests).
//! * The connection lock is a `std::sync::Mutex` in [`SharedConnection`]; the
//!   `last_seen` timestamp is a plain field under that lock instead of an
//!   atomic. Firewall handlers, packet queues, inspectors, prompts, metrics
//!   and PID hints (`pidhint.go`) are not part of this crate.
//! * Process attribution, profiles, the resolver's IP info and the captive
//!   portal are injected through [`Environment`]; profiles are not part of the
//!   process type yet.
//! * The cleaner is a plain method ([`Network::clean_connections`]); the worker
//!   loop and the process-storage cleanup are driven by the caller. The
//!   Windows-only domain wait in the connection gathering is not ported.
//! * Connection updates are pushed to subscribers through the injected
//!   database controller; the database exposes processes and connections like
//!   Go, but write operations are not implemented.

mod clean;
mod connection;
mod database;
mod dns;
mod env;
mod network;
mod store;
mod types;

pub use clean::{
    CLEANER_TICK, DELETE_CONNS_AFTER_ENDED_THRESHOLD,
    DELETE_INCOMPLETE_CONNS_AFTER_STARTED_THRESHOLD, END_CONNS_AFTER_INACTIVE_FOR,
    END_ICMP_CONNS_AFTER_INACTIVE_FOR,
};
pub use connection::{
    Connection, DnsRequestContext, JsonIp, ProcessContext, Reason, SharedConnection,
    SplitTunContext,
};
pub use database::{
    parse_db_key, process_record, register_as_database, NetworkStorage, ParsedKey, DATABASE_NAME,
};
pub use dns::{
    is_supported_dns_record_type, OpenDnsQuery, DNS_TYPE_A, DNS_TYPE_AAAA, DNS_TYPE_HTTPS,
    DNS_TYPE_SVCB, OPEN_DNS_REQUEST_LIMIT,
};
pub use env::{
    CaptivePortal, CaptivePortalGetter, Clock, DomainLookup, EntityFactory, Environment,
    IpInfoNotFound, LocalProfileInfo, ManualClock, NoDomains, NoProfiles, ProcessResolver,
    ProfileLookup, ResolvedDomain, SystemClock, SystemProcessResolver,
    IP_INFO_PROFILE_SCOPE_GLOBAL,
};
pub use network::{make_key, Network, DB_SCOPE_DNS, DB_SCOPE_IP};
pub use store::ConnectionStore;
pub use types::{
    canonical_ip, create_connection_id, format_ip, protocol, version, ConnectionType, PacketInfo,
    Verdict, INCOMING_HOST, INCOMING_INTERNET, INCOMING_INVALID, INCOMING_LAN, PEER_HOST,
    PEER_INTERNET, PEER_INVALID, PEER_LAN,
};
