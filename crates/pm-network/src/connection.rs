//! The connection model, a port of the data part of Go `network/connection.go`.

use std::collections::BTreeMap;
use std::fmt;
use std::net::IpAddr;
use std::sync::{Arc, Mutex};

use pm_intel::Entity;
use pm_process::Process;
use pm_record::{Key, Meta, Record};
use pm_resolver::{IpScope, ResolverInfo};
use serde::{Serialize, Serializer};
use serde_json::Value;

use crate::env::{NoProfiles, ProfileLookup};
use crate::types::{canonical_ip, ConnectionType, Verdict};

/// A connection that is shared between the store, the database and workers.
///
/// Go locks the connection with an embedded mutex; this is the same lock.
pub type SharedConnection = Arc<Mutex<Connection>>;

/// Serializes an optional IP like Go `net.IP`: empty string if unset.
#[derive(Debug, Clone, Copy, Default, PartialEq, Eq)]
pub struct JsonIp(pub Option<IpAddr>);

impl Serialize for JsonIp {
    fn serialize<S: Serializer>(&self, serializer: S) -> Result<S::Ok, S::Error> {
        match self.0 {
            Some(ip) => serializer.collect_str(&canonical_ip(ip)),
            None => serializer.serialize_str(""),
        }
    }
}

/// Additional information about the process that initiated a connection.
#[derive(Debug, Clone, Default, PartialEq, Eq, Serialize)]
pub struct ProcessContext {
    #[serde(rename = "ProcessName")]
    pub process_name: String,
    #[serde(rename = "ProfileName")]
    pub profile_name: String,
    #[serde(rename = "BinaryPath")]
    pub binary_path: String,
    #[serde(rename = "CmdLine")]
    pub cmd_line: String,
    #[serde(rename = "PID")]
    pub pid: i32,
    #[serde(rename = "CreatedAt")]
    pub created_at: i64,
    #[serde(rename = "Profile")]
    pub profile: String,
    #[serde(rename = "Source")]
    pub source: String,
}

/// The split tunnel a connection is routed through.
#[derive(Debug, Clone, Default, PartialEq, Eq, Serialize)]
pub struct SplitTunContext {
    #[serde(rename = "Interface")]
    pub interface: String,
    #[serde(rename = "IP")]
    pub ip: JsonIp,
    /// Upstream proxy without credentials; omitted when empty.
    #[serde(rename = "Proxy", skip_serializing_if = "String::is_empty")]
    pub proxy: String,
}

/// Information about the DNS request that probably resolved the connection's
/// IP, a port of Go `resolver.DNSRequestContext`.
///
/// `modified` and `expires` hold RFC 3339 timestamps like Go's `time.Time`
/// JSON form.
#[derive(Debug, Clone, Default, PartialEq, Eq, Serialize)]
pub struct DnsRequestContext {
    #[serde(rename = "Domain")]
    pub domain: String,
    #[serde(rename = "Question")]
    pub question: String,
    #[serde(rename = "RCode")]
    pub rcode: String,
    #[serde(rename = "ServedFromCache")]
    pub served_from_cache: bool,
    #[serde(rename = "RequestingNew")]
    pub requesting_new: bool,
    #[serde(rename = "IsBackup")]
    pub is_backup: bool,
    #[serde(rename = "Filtered")]
    pub filtered: bool,
    #[serde(rename = "Modified")]
    pub modified: String,
    #[serde(rename = "Expires")]
    pub expires: String,
}

/// Justification of a verdict.
#[derive(Debug, Clone, Default, PartialEq, Serialize)]
pub struct Reason {
    #[serde(rename = "Msg")]
    pub msg: String,
    /// Configuration option key of the setting responsible for the verdict.
    #[serde(rename = "OptionKey")]
    pub option_key: String,
    /// Profile that held the responsible setting.
    #[serde(rename = "Profile")]
    pub profile: String,
    /// Additional reason-specific information.
    #[serde(rename = "Context")]
    pub context: Option<Value>,
}

/// A network connection or DNS request.
///
/// All fields that Go exports are serialized in Go order with Go names; the
/// rest is runtime state.
pub struct Connection {
    pub id: String,
    pub conn_type: ConnectionType,
    pub external: bool,
    pub scope: String,
    pub ip_version: u8,
    pub inbound: bool,
    pub ip_protocol: u8,
    pub local_ip: Option<IpAddr>,
    pub local_ip_scope: IpScope,
    pub local_port: u16,
    pub pid: i32,
    pub entity: Option<Entity>,
    pub resolver: Option<ResolverInfo>,
    pub verdict: Verdict,
    pub connection_established: bool,
    pub reason: Reason,
    pub started: i64,
    pub ended: i64,
    pub verdict_permanent: bool,
    pub inspecting: bool,
    pub tunneled: bool,
    pub encrypted: bool,
    pub tunnel_opts: Option<Value>,
    pub process_context: ProcessContext,
    pub dns_context: Option<DnsRequestContext>,
    pub split_tun_context: Option<SplitTunContext>,
    pub history_enabled: bool,
    pub bandwidth_enabled: bool,
    pub bytes_received: u64,
    pub bytes_sent: u64,
    pub internal: bool,
    pub profile_revision_counter: u64,

    /// Record metadata.
    pub meta: Meta,
    /// Database key; set when the connection is first saved.
    pub key: Option<Key>,
    /// Unix seconds when the connection was last seen.
    pub last_seen: i64,
    /// Set once all information is available and an actual packet was seen.
    pub data_complete: bool,
    /// The process that initiated the connection.
    pub process: Option<Arc<Process>>,
    pub save_when_finished: bool,
    pub profiles: Arc<dyn ProfileLookup>,
}

impl Default for Connection {
    fn default() -> Connection {
        Connection {
            id: String::new(),
            conn_type: ConnectionType::Undefined,
            external: false,
            scope: String::new(),
            ip_version: 0,
            inbound: false,
            ip_protocol: 0,
            local_ip: None,
            local_ip_scope: IpScope::Undefined,
            local_port: 0,
            pid: 0,
            entity: None,
            resolver: None,
            verdict: Verdict::Undecided,
            connection_established: false,
            reason: Reason::default(),
            started: 0,
            ended: 0,
            verdict_permanent: false,
            inspecting: false,
            tunneled: false,
            encrypted: false,
            tunnel_opts: None,
            process_context: ProcessContext::default(),
            dns_context: None,
            split_tun_context: None,
            history_enabled: false,
            bandwidth_enabled: false,
            bytes_received: 0,
            bytes_sent: 0,
            internal: false,
            profile_revision_counter: 0,
            meta: Meta::default(),
            key: None,
            last_seen: 0,
            data_complete: false,
            process: None,
            save_when_finished: false,
            profiles: Arc::new(NoProfiles),
        }
    }
}

impl fmt::Debug for Connection {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.debug_struct("Connection")
            .field("id", &self.id)
            .field("conn_type", &self.conn_type)
            .field("verdict", &self.verdict)
            .finish_non_exhaustive()
    }
}

impl fmt::Display for Connection {
    /// Like Go `Connection.String`.
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        let (Some(process), Some(entity)) = (&self.process, &self.entity) else {
            return f.write_str(&self.id);
        };
        let ip = crate::types::format_ip(entity.ip);
        if self.inbound {
            write!(f, "{process} <- {ip}")
        } else if !entity.domain.is_empty() {
            write!(f, "{process} to {} ({ip})", entity.domain)
        } else {
            write!(f, "{process} -> {ip}")
        }
    }
}

/// Entity in its Go JSON form.
#[derive(Serialize)]
struct EntityJson<'a> {
    #[serde(rename = "IP")]
    ip: JsonIp,
    #[serde(rename = "IPScope")]
    ip_scope: IpScope,
    #[serde(rename = "Protocol")]
    protocol: u8,
    #[serde(rename = "Port")]
    port: u16,
    #[serde(rename = "Domain")]
    domain: &'a str,
    #[serde(rename = "ReverseDomain")]
    reverse_domain: &'a str,
    #[serde(rename = "CNAME")]
    cname: Option<&'a [String]>,
    #[serde(rename = "Country")]
    country: &'a str,
    #[serde(rename = "Coordinates")]
    coordinates: &'a Option<pm_geoip::Coordinates>,
    #[serde(rename = "ASN")]
    asn: u32,
    #[serde(rename = "ASOrg")]
    as_org: &'a str,
    #[serde(rename = "LocationError")]
    location_error: &'a str,
    #[serde(rename = "BlockedByLists")]
    blocked_by_lists: Option<&'a [String]>,
    #[serde(rename = "BlockedEntities")]
    blocked_entities: Option<&'a [String]>,
    #[serde(rename = "ListOccurences")]
    list_occurences: Option<&'a BTreeMap<String, Vec<String>>>,
    #[serde(rename = "ListsError")]
    lists_error: &'a str,
}

/// Go serializes empty (nil) slices and maps as `null`; Rust cannot tell nil
/// from empty, so empty collections are written as `null`.
fn non_empty<T>(slice: &[T]) -> Option<&[T]> {
    if slice.is_empty() {
        None
    } else {
        Some(slice)
    }
}

/// Like [`non_empty`], but `matched` slices are non-nil in Go and stay `[]`.
fn nil_unless<T>(slice: &[T], matched: bool) -> Option<&[T]> {
    if matched {
        Some(slice)
    } else {
        non_empty(slice)
    }
}

impl<'a> EntityJson<'a> {
    fn new(entity: &'a Entity) -> EntityJson<'a> {
        EntityJson {
            ip: JsonIp(entity.ip),
            ip_scope: entity.ip_scope,
            protocol: entity.protocol,
            port: entity.port,
            domain: &entity.domain,
            reverse_domain: &entity.reverse_domain,
            cname: non_empty(&entity.cname),
            country: &entity.country,
            coordinates: &entity.coordinates,
            asn: entity.asn,
            as_org: &entity.as_org,
            location_error: &entity.location_error,
            blocked_by_lists: nil_unless(&entity.blocked_by_lists, entity.lists_matched),
            blocked_entities: nil_unless(&entity.blocked_entities, entity.lists_matched),
            list_occurences: if entity.list_occurences.is_empty() {
                None
            } else {
                Some(&entity.list_occurences)
            },
            lists_error: &entity.lists_error,
        }
    }
}

/// Returns the Go JSON form of an entity, for records that embed it (for
/// example prompt notifications).
pub fn entity_to_json(entity: &pm_intel::Entity) -> serde_json::Value {
    serde_json::to_value(EntityJson::new(entity)).unwrap_or(serde_json::Value::Null)
}

#[derive(Serialize)]
struct ConnectionJson<'a> {
    #[serde(rename = "ID")]
    id: &'a str,
    #[serde(rename = "Type")]
    conn_type: ConnectionType,
    #[serde(rename = "External")]
    external: bool,
    #[serde(rename = "Scope")]
    scope: &'a str,
    #[serde(rename = "IPVersion")]
    ip_version: u8,
    #[serde(rename = "Inbound")]
    inbound: bool,
    #[serde(rename = "IPProtocol")]
    ip_protocol: u8,
    #[serde(rename = "LocalIP")]
    local_ip: JsonIp,
    #[serde(rename = "LocalIPScope")]
    local_ip_scope: IpScope,
    #[serde(rename = "LocalPort")]
    local_port: u16,
    #[serde(rename = "PID")]
    pid: i32,
    #[serde(rename = "Entity")]
    entity: Option<EntityJson<'a>>,
    #[serde(rename = "Resolver")]
    resolver: &'a Option<ResolverInfo>,
    #[serde(rename = "Verdict")]
    verdict: Verdict,
    #[serde(rename = "ConnectionEstablished")]
    connection_established: bool,
    #[serde(rename = "Reason")]
    reason: &'a Reason,
    #[serde(rename = "Started")]
    started: i64,
    #[serde(rename = "Ended")]
    ended: i64,
    #[serde(rename = "VerdictPermanent")]
    verdict_permanent: bool,
    #[serde(rename = "Inspecting")]
    inspecting: bool,
    #[serde(rename = "Tunneled")]
    tunneled: bool,
    #[serde(rename = "Encrypted")]
    encrypted: bool,
    #[serde(rename = "TunnelOpts")]
    tunnel_opts: &'a Option<Value>,
    #[serde(rename = "ProcessContext")]
    process_context: &'a ProcessContext,
    #[serde(rename = "DNSContext")]
    dns_context: &'a Option<DnsRequestContext>,
    /// SPN tunnel contexts are not ported; Go writes `null` without a tunnel.
    #[serde(rename = "TunnelContext")]
    tunnel_context: Option<()>,
    #[serde(rename = "SplitTunContext")]
    split_tun_context: &'a Option<SplitTunContext>,
    #[serde(rename = "HistoryEnabled")]
    history_enabled: bool,
    #[serde(rename = "BandwidthEnabled")]
    bandwidth_enabled: bool,
    #[serde(rename = "BytesReceived")]
    bytes_received: u64,
    #[serde(rename = "BytesSent")]
    bytes_sent: u64,
    #[serde(rename = "Internal")]
    internal: bool,
    #[serde(rename = "ProfileRevisionCounter")]
    profile_revision_counter: u64,
}

impl Serialize for Connection {
    fn serialize<S: Serializer>(&self, serializer: S) -> Result<S::Ok, S::Error> {
        ConnectionJson {
            id: &self.id,
            conn_type: self.conn_type,
            external: self.external,
            scope: &self.scope,
            ip_version: self.ip_version,
            inbound: self.inbound,
            ip_protocol: self.ip_protocol,
            local_ip: JsonIp(self.local_ip),
            local_ip_scope: self.local_ip_scope,
            local_port: self.local_port,
            pid: self.pid,
            entity: self.entity.as_ref().map(EntityJson::new),
            resolver: &self.resolver,
            verdict: self.verdict,
            connection_established: self.connection_established,
            reason: &self.reason,
            started: self.started,
            ended: self.ended,
            verdict_permanent: self.verdict_permanent,
            inspecting: self.inspecting,
            tunneled: self.tunneled,
            encrypted: self.encrypted,
            tunnel_opts: &self.tunnel_opts,
            process_context: &self.process_context,
            dns_context: &self.dns_context,
            tunnel_context: None,
            split_tun_context: &self.split_tun_context,
            history_enabled: self.history_enabled,
            bandwidth_enabled: self.bandwidth_enabled,
            bytes_received: self.bytes_received,
            bytes_sent: self.bytes_sent,
            internal: self.internal,
            profile_revision_counter: self.profile_revision_counter,
        }
        .serialize(serializer)
    }
}

impl Connection {
    /// Sets the local IP together with its scope.
    pub fn set_local_ip(&mut self, ip: Option<IpAddr>) {
        self.local_ip = ip;
        self.local_ip_scope = match ip {
            Some(ip) => pm_netstate::ip_scope(canonical_ip(ip)),
            None => IpScope::Undefined,
        };
    }

    /// Returns the PID of the owning process, falling back to `pid`.
    pub fn process_pid(&self) -> i32 {
        self.process
            .as_ref()
            .map_or(self.pid, |process| process.pid)
    }

    /// Updates the record metadata, like Go `UpdateMeta`.
    pub fn update_meta(&mut self, now: i64) {
        self.meta.update(now);
    }

    /// Checks which connection related features may and should be used.
    pub fn update_features(&mut self) {
        self.history_enabled = false;
        let localhost = self
            .entity
            .as_ref()
            .is_some_and(|entity| entity.ip_scope.is_localhost());
        if !self.internal && !localhost {
            if let Some(process) = &self.process {
                if let Some(profile) = self.profiles.local_profile(process) {
                    self.history_enabled = profile.enable_history;
                }
            }
        }
        // Deliberately follows the user's local Go change (always true), not
        // upstream (false).
        self.bandwidth_enabled = true;
    }

    /// Sets a new verdict and its reason, like Go `SetVerdict`.
    pub fn set_verdict(
        &mut self,
        verdict: Verdict,
        reason: &str,
        reason_option_key: &str,
        reason_context: Option<Value>,
    ) {
        self.verdict = verdict;
        self.reason.msg = reason.to_string();
        self.reason.context = reason_context;
        self.reason.option_key.clear();
        self.reason.profile.clear();

        if !reason_option_key.is_empty() {
            if let Some(process) = &self.process {
                if let Some(source) = self.profiles.profile_source(process, reason_option_key) {
                    self.reason.option_key = reason_option_key.to_string();
                    self.reason.profile = source;
                }
            }
        }
    }

    pub fn accept(&mut self, reason: &str, option_key: &str) {
        self.set_verdict(Verdict::Accept, reason, option_key, None);
    }

    pub fn block(&mut self, reason: &str, option_key: &str) {
        self.set_verdict(Verdict::Block, reason, option_key, None);
    }

    pub fn drop_conn(&mut self, reason: &str, option_key: &str) {
        self.set_verdict(Verdict::Drop, reason, option_key, None);
    }

    /// Blocks or drops depending on the connection direction.
    pub fn deny(&mut self, reason: &str, option_key: &str) {
        if self.inbound {
            self.drop_conn(reason, option_key);
        } else {
            self.block(reason, option_key);
        }
    }

    pub fn failed(&mut self, reason: &str, option_key: &str) {
        self.set_verdict(Verdict::Failed, reason, option_key, None);
    }

    /// Returns the verdict as a verb.
    pub fn verdict_verb(&self) -> &'static str {
        self.verdict.verb()
    }

    /// Returns the database record of the connection.
    ///
    /// Deleted connections have no payload. Returns `None` while the key is
    /// not set yet.
    pub fn to_record(&self) -> Option<Record> {
        let key = self.key.clone()?;
        let data = if self.meta.is_deleted() {
            Vec::new()
        } else {
            match pm_dsd::gojson::to_vec(self) {
                Ok(data) => data,
                Err(error) => {
                    tracing::error!(%error, id = %self.id, "network: failed to serialize connection");
                    return None;
                }
            }
        };
        Some(Record {
            key,
            meta: self.meta,
            format: pm_dsd::format::JSON,
            data,
        })
    }
}
