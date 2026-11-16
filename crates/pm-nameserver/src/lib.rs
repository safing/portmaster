//! Port of the Go `service/nameserver` package: the local DNS server.
//!
//! The server listens on UDP only, like Go. Everything that lives outside the
//! package is injected: the resolver ([`DnsResolve`]), the process
//! attribution ([`RequestAttributor`]), the firewall steps
//! ([`FirewallHook`], permissive by default) and the environment
//! ([`ServerEnv`]).
//!
//! # Compatibility notes
//!
//! * A firewall that wants to craft the reply itself returns it in
//!   [`FilterOutcome`]; Go smuggles it through `Reason.Context`, which is JSON
//!   in the Rust connection.
//! * The server library's packet rules are reproduced in
//!   [`Nameserver::handle_packet`]: short packets and responses are ignored,
//!   a bad header or body gets FORMERR, other opcodes NOTIMP.
//! * Replies are not truncated, like Go; an oversized reply is retried
//!   without the extra section.
//! * The `DNSContext` timestamps are written in UTC.
//! * Metrics are not ported. Notifications are left to the caller: listener
//!   problems are reported as [`ListenerEvent`]s.
//! * `QueryPortmasterEnv` is not exposed by the resolver service yet, so the
//!   alternative-name lookup only uses the cache there.
//! * The "is this IP mine" check of [`SystemEnv`] binds a socket instead of
//!   reading the interface table.

pub mod config;
pub mod conflict;
pub mod env;
pub mod failing;
pub mod handler;
pub mod hooks;
pub mod matcher;
pub mod responder;
pub mod server;

pub use config::{
    default_nameserver_address, parse_listen_addresses, register_options, ListenAddressError,
    ListenAddresses, CFG_DEFAULT_NAMESERVER_ADDRESS_KEY,
};
pub use conflict::{
    find_conflicting_process, ConflictingProcess, ListeningProcessLookup, NetstateLookup,
};
pub use env::{ServerEnv, SystemEnv};
pub use handler::{Nameserver, NameserverDeps};
pub use hooks::{
    DnsRequestInfo, DnsResolve, FilterOutcome, FirewallHook, NetworkAttributor, PermissiveHook,
    RequestAttributor,
};
pub use matcher::NameserverIpMatcher;
pub use responder::{Responder, RrProvider, StaticResponder};
pub use server::{
    start, EventSink, ListenerEvent, ListenerFailure, Running, StartOptions,
    COMPATIBILITY_DOCS_URL,
};
