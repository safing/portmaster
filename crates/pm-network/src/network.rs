//! The network module state: connection stores, saving and construction of
//! connections.

use std::collections::HashMap;
use std::net::IpAddr;
use std::sync::{Arc, Mutex, RwLock};
use std::time::{Duration, UNIX_EPOCH};

use pm_database::Controller;
use pm_netstate::{IpScope as NsIpScope, UNDEFINED_PROCESS_ID};
use pm_process::Process;
use pm_record::Key;

use crate::connection::{Connection, ProcessContext, SharedConnection};
use crate::env::{Environment, IP_INFO_PROFILE_SCOPE_GLOBAL};
use crate::store::ConnectionStore;
use crate::types::{
    canonical_ip, protocol, version, ConnectionType, PacketInfo, Verdict, INCOMING_HOST,
    INCOMING_INTERNET, INCOMING_INVALID, INCOMING_LAN, PEER_HOST, PEER_INTERNET, PEER_INVALID,
    PEER_LAN,
};

/// Database scope of DNS requests.
pub const DB_SCOPE_DNS: &str = "dns";
/// Database scope of IP connections.
pub const DB_SCOPE_IP: &str = "ip";

/// Start timestamps before this (2020-01-01 UTC) are considered invalid.
const TOO_OLD_TIMESTAMP: i64 = 1_577_836_800;

/// Seconds after which an ended DNS request connection is no longer used for
/// attributing new requests.
pub(crate) const DNS_REQUEST_CONN_GRACE: i64 = 3;

/// Builds a database key (without the database name), like Go `makeKey`.
///
/// Process records (empty scope) are `tree/<PID>`.
pub fn make_key(pid: i32, scope: &str, id: &str) -> String {
    if scope.is_empty() {
        return format!("tree/{pid}");
    }
    format!("tree/{pid}/{scope}/{id}")
}

/// The state of the network module.
pub struct Network {
    pub(crate) env: Environment,
    pub(crate) conns: ConnectionStore,
    pub(crate) dns_conns: ConnectionStore,
    pub(crate) db: RwLock<Option<Arc<Controller>>>,
    pub(crate) dns_request_conns: Mutex<HashMap<String, SharedConnection>>,
    pub(crate) open_dns_requests: Mutex<HashMap<String, SharedConnection>>,
}

impl Network {
    pub fn new(env: Environment) -> Arc<Network> {
        Arc::new(Network {
            env,
            conns: ConnectionStore::new(),
            dns_conns: ConnectionStore::new(),
            db: RwLock::new(None),
            dns_request_conns: Mutex::new(HashMap::new()),
            open_dns_requests: Mutex::new(HashMap::new()),
        })
    }

    pub fn env(&self) -> &Environment {
        &self.env
    }

    /// Sets the controller that receives connection updates.
    pub fn set_db_controller(&self, controller: Arc<Controller>) {
        self.env
            .processes
            .set_db_controller(Arc::clone(&controller));
        *self.db.write().expect("lock poisoned") = Some(controller);
    }

    /// Returns the IP connection with the given ID.
    pub fn get_connection(&self, id: &str) -> Option<SharedConnection> {
        self.conns.get(id)
    }

    /// Returns all IP connections.
    pub fn get_all_connections(&self) -> Vec<SharedConnection> {
        self.conns.list()
    }

    /// Returns the DNS request connection with the given ID.
    pub fn get_dns_connection(&self, id: &str) -> Option<SharedConnection> {
        self.dns_conns.get(id)
    }

    /// Returns all DNS request connections.
    pub fn get_all_dns_connections(&self) -> Vec<SharedConnection> {
        self.dns_conns.list()
    }

    fn store_of(&self, conn_type: ConnectionType) -> &ConnectionStore {
        if conn_type == ConnectionType::Ip {
            &self.conns
        } else {
            &self.dns_conns
        }
    }

    /// Saves the connection and propagates the change, like Go `Save`.
    /// The connection must not be locked by the caller; see
    /// [`Network::save_locked`].
    pub fn save(&self, shared: &SharedConnection) {
        let mut conn = shared.lock().expect("lock poisoned");
        self.save_locked(shared, &mut conn);
    }

    /// Like [`Network::save`] for callers that hold the connection lock.
    pub fn save_locked(&self, shared: &SharedConnection, conn: &mut Connection) {
        conn.update_meta(self.env.clock.now());

        if matches!(
            conn.verdict,
            Verdict::Accept | Verdict::RerouteToNameserver | Verdict::RerouteToSplitTun
        ) {
            conn.connection_established = true;
        }

        // Do not save or expose until the data is complete.
        if !conn.data_complete {
            return;
        }

        if conn.key.is_none() {
            let scope = if conn.conn_type == ConnectionType::Dns {
                DB_SCOPE_DNS
            } else {
                DB_SCOPE_IP
            };
            conn.key = Some(Key::new(
                "network",
                &make_key(conn.process_pid(), scope, &conn.id),
            ));
            self.store_of(conn.conn_type).add(&conn.id, shared);
        }

        self.push_update(conn);
    }

    /// Removes the connection from its store and propagates the deletion, like
    /// Go `delete`. The caller holds the connection lock.
    pub fn delete_locked(&self, conn: &mut Connection) {
        self.store_of(conn.conn_type).delete(&conn.id);
        conn.meta.delete(self.env.clock.now());

        // Only complete connections were exposed before.
        if conn.data_complete {
            self.push_update(conn);
        }
    }

    fn push_update(&self, conn: &Connection) {
        let Some(controller) = self.db.read().expect("lock poisoned").clone() else {
            return;
        };
        if let Some(record) = conn.to_record() {
            controller.push_update(&record);
        }
    }

    /// Returns the process context of a process.
    pub fn process_context(&self, process: &Process) -> ProcessContext {
        let mut context = ProcessContext {
            process_name: process.name.clone(),
            binary_path: process.path.clone(),
            cmd_line: process.cmd_line.clone(),
            pid: process.pid,
            created_at: process.created_at,
            ..ProcessContext::default()
        };
        match self.env.profiles.local_profile(process) {
            Some(profile) => {
                context.profile_name = profile.name;
                context.profile = profile.id;
                context.source = profile.source;
            }
            None => tracing::debug!(%process, "network: process has no profile"),
        }
        context
    }

    fn new_dns_connection(
        &self,
        fqdn: &str,
        cnames: Vec<String>,
        conn_id: &str,
        process: Arc<Process>,
        external: bool,
    ) -> SharedConnection {
        let now = self.env.clock.now();
        let mut entity = (self.env.new_entity)();
        entity.domain = fqdn.to_string();
        entity.cname = cnames;
        entity.ip_scope = NsIpScope::Global; // Default scope for DNS requests.

        let mut conn = Connection {
            id: conn_id.to_string(),
            conn_type: ConnectionType::Dns,
            external,
            scope: fqdn.to_string(),
            pid: process.pid,
            entity: Some(entity),
            process_context: self.process_context(&process),
            started: now,
            ended: now,
            last_seen: now,
            data_complete: true,
            profiles: Arc::clone(&self.env.profiles),
            ..Connection::default()
        };

        if let Some(profile) = self.env.profiles.local_profile(&process) {
            conn.internal = profile.internal;
            conn.update_features();
        }
        conn.process = Some(process);

        // DNS requests are saved by the nameserver depending on the result of
        // the query.
        conn.update_meta(now);
        Arc::new(Mutex::new(conn))
    }

    /// Creates a connection for a DNS request, like Go
    /// `NewConnectionFromDNSRequest`.
    pub fn new_connection_from_dns_request(
        &self,
        fqdn: &str,
        cnames: Vec<String>,
        conn_id: &str,
        local_ip: IpAddr,
        local_port: u16,
    ) -> SharedConnection {
        let now = self.env.clock.now();
        let mut info = PacketInfo {
            inbound: false, // outbound, as we look for the process of the source
            in_tunnel: false,
            version: if canonical_ip(local_ip).is_ipv4() {
                version::V4
            } else {
                version::V6
            },
            protocol: protocol::UDP,
            src_port: local_port,
            dst_port: 0,
            src: Some(local_ip),
            dst: None, // do not record direction
            pid: UNDEFINED_PROCESS_ID,
            seen_at: UNIX_EPOCH + Duration::from_secs(now.max(0) as u64),
        };

        // Check if the DNS request connection was reported with process info.
        let mut process: Option<Arc<Process>> = None;
        if let Some(dns_conn) = self.get_dns_request_connection(&info) {
            let dns_conn = dns_conn.lock().expect("lock poisoned");
            if dns_conn.pid < 0 {
                // Process is not identified or special.
            } else if dns_conn.ended > 0 && dns_conn.ended < now - DNS_REQUEST_CONN_GRACE {
                tracing::debug!(conn = %dns_conn, fqdn, "network: found ended dns request connection");
            } else {
                info.pid = dns_conn.pid;
                // Inherit the process itself, as the PID may already be reused.
                process = dns_conn.process.clone();
            }
        }

        if info.pid == UNDEFINED_PROCESS_ID {
            info.pid = self.env.processes.pid_of_connection(&mut info).0;
        }
        let process = process.unwrap_or_else(|| self.env.processes.process(info.pid));

        self.new_dns_connection(fqdn, cnames, conn_id, process, false)
    }

    /// Creates a connection for a DNS request of another device, like Go
    /// `NewConnectionFromExternalDNSRequest`.
    pub fn new_connection_from_external_dns_request(
        &self,
        fqdn: &str,
        cnames: Vec<String>,
        conn_id: &str,
        remote_ip: IpAddr,
    ) -> SharedConnection {
        let host = self.env.processes.network_host(remote_ip);
        self.new_dns_connection(fqdn, cnames, conn_id, host, true)
    }

    /// Creates an incomplete connection with minimal information and adds it
    /// to the store to avoid duplicates, like Go `NewIncompleteConnection`.
    /// It is not propagated until the data is complete.
    pub fn new_incomplete_connection(&self, info: &PacketInfo) -> SharedConnection {
        let now = self.env.clock.now();
        let mut started = info
            .seen_at
            .duration_since(UNIX_EPOCH)
            .map_or(0, |elapsed| elapsed.as_secs() as i64);
        if started < TOO_OLD_TIMESTAMP {
            started = now;
        }

        let mut conn = Connection {
            id: info.connection_id(),
            conn_type: ConnectionType::Ip,
            ip_version: info.version,
            ip_protocol: info.protocol,
            started,
            pid: info.pid,
            inbound: info.inbound,
            last_seen: started,
            profiles: Arc::clone(&self.env.profiles),
            ..Connection::default()
        };
        conn.update_meta(now);

        let id = conn.id.clone();
        let shared = Arc::new(Mutex::new(conn));
        self.conns.add(&id, &shared);
        shared
    }

    /// Gathers information on the process and remote entity, like Go
    /// `GatherConnectionInfo`. The caller holds the connection lock.
    ///
    /// With `info_only` set the packet is not a real packet and only the
    /// entity and the PID are determined.
    pub fn gather_connection_info(
        &self,
        conn: &mut Connection,
        info: &PacketInfo,
        info_only: bool,
    ) {
        if conn.entity.is_none() {
            let mut entity = (self.env.new_entity)();
            entity.ip = info.remote_ip().map(canonical_ip);
            entity.protocol = info.protocol;
            entity.port = info.remote_port();
            entity.init(info.dst_port);

            conn.set_local_ip(info.local_ip());
            conn.local_port = info.local_port();

            conn.scope = connection_scope(conn.inbound, entity.ip_scope).to_string();
            conn.entity = Some(entity);
        }

        if conn.pid == UNDEFINED_PROCESS_ID {
            let mut lookup = info.clone();
            let (pid, inbound) = self.env.processes.pid_of_connection(&mut lookup);
            conn.pid = pid;
            conn.inbound = inbound;
        }

        // Only get process and profile with the first real packet.
        if info_only {
            return;
        }

        if conn.process.is_none() {
            conn.process = Some(self.env.processes.process(conn.pid));
        }
        let process = conn.process.clone().expect("process was just set");
        let local_profile = self.env.profiles.local_profile(&process);

        if conn.profile_revision_counter == 0 {
            conn.process_context = self.process_context(&process);
            conn.profile_revision_counter = local_profile.as_ref().map_or(0, |p| p.revision_cnt);
            if let Some(profile) = &local_profile {
                conn.internal = profile.internal;
                conn.update_features();
            }
        }

        // Find the domain and DNS context of the entity.
        let domain_empty = conn.entity.as_ref().is_some_and(|e| e.domain.is_empty());
        if let (Some(profile), true, Some(remote_ip)) =
            (&local_profile, domain_empty, info.remote_ip())
        {
            let remote_ip = canonical_ip(remote_ip);
            let resolved = self
                .env
                .domains
                .most_recent_domain(&profile.id, remote_ip)
                // The DNS request may have gone through the system resolver.
                .or_else(|| {
                    self.env
                        .domains
                        .most_recent_domain(IP_INFO_PROFILE_SCOPE_GLOBAL, remote_ip)
                });
            if let Some(resolved) = resolved {
                conn.scope = resolved.domain.clone();
                if let Some(entity) = conn.entity.as_mut() {
                    entity.domain = resolved.domain.clone();
                    entity.cname = resolved.cnames;
                }
                conn.dns_context = resolved.dns_context;
                conn.resolver = resolved.resolver;
                self.remove_open_dns_request(process.pid, &resolved.domain);
            }
        }

        // Check if the destination is the captive portal.
        if conn.entity.as_ref().is_some_and(|e| e.domain.is_empty()) {
            if let Some(portal) = (self.env.captive_portal)() {
                if info.remote_ip().map(canonical_ip) == Some(canonical_ip(portal.ip)) {
                    conn.scope = portal.domain.clone();
                    if let Some(entity) = conn.entity.as_mut() {
                        entity.domain = portal.domain;
                    }
                }
            }
        }

        // All required data is available for an actual packet.
        if conn.entity.is_some() {
            conn.data_complete = true;
        }
        conn.save_when_finished = true;
    }

    /// Creates a connection from the first packet of a flow: an incomplete
    /// connection that is gathered right away. The firewall decides on the
    /// verdict and calls [`Network::save`] afterwards.
    pub fn new_connection_from_first_packet(&self, info: &PacketInfo) -> SharedConnection {
        let shared = self.new_incomplete_connection(info);
        {
            let mut conn = shared.lock().expect("lock poisoned");
            self.gather_connection_info(&mut conn, info, false);
        }
        shared
    }
}

fn connection_scope(inbound: bool, scope: NsIpScope) -> &'static str {
    let (host, lan, internet, invalid) = if inbound {
        (
            INCOMING_HOST,
            INCOMING_LAN,
            INCOMING_INTERNET,
            INCOMING_INVALID,
        )
    } else {
        (PEER_HOST, PEER_LAN, PEER_INTERNET, PEER_INVALID)
    };
    match scope {
        NsIpScope::HostLocal => host,
        NsIpScope::LinkLocal | NsIpScope::SiteLocal | NsIpScope::LocalMulticast => lan,
        NsIpScope::Global | NsIpScope::GlobalMulticast => internet,
        NsIpScope::Undefined | NsIpScope::Invalid => invalid,
    }
}
