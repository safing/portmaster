//! Linux DNS monitor of the systemd-resolved varlink query events.
//!
//! Port of Go `service/firewall/interception/dnsmonitor` (`varlinktypes.go`,
//! `eventlistener_linux.go` and `module.go`). Only the Linux/varlink part is
//! ported; the Windows ETW implementation is intentionally left out.
//!
//! The event source and the IP-info sink are traits, so the parser and the
//! event handling can be tested without systemd-resolved, a varlink socket or
//! root privileges. Resolver types and the IP-info store come from
//! `pm-resolver`; the network-change notification uses `pm-netenv`.

pub mod event;
pub mod monitor;
pub mod source;
pub mod varlinktypes;

pub use event::{parse_event, parse_query_result, EventParseError, VarlinkEvent};
pub use monitor::{
    system_resolver_info, DnsInfoSink, DnsMonitor, DnsMonitorError, EventOutcome,
    IpInfoStoreSink, NoopSelfCheck, RunningDnsMonitor, SelfCheckSink, RESTART_LIMIT,
};
pub use source::{
    DnsEventSource, FakeEventSource, ShutdownFn, SourceError, SystemdVarlinkSource,
    DEFAULT_SYSTEMD_SOCKET, SUBSCRIBE_METHOD,
};
pub use varlinktypes::{Answer, QueryResult, ResourceKey, ResourceRecord};
