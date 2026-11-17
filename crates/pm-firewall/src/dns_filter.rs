//! Port of Go `service/firewall/dns.go`: filtering of DNS responses and the
//! IP to domain cache, plus [`FirewallDnsHook`], the nameserver's firewall step.
//!
//! # Compatibility notes
//!
//! * Go reads the resolver of a response without a nil check; a response
//!   without resolver info is treated as coming from an unscoped resolver.
//! * [`IpInfoDomains`] lives here and not in `pm-resolver`, because
//!   `pm-network` (which owns `DomainLookup`) depends on `pm-resolver`.
//! * `UpdateIPsAndCNAMEs` sets the CNAMEs on the entity of DNS request
//!   connections. [`update_ips_and_cnames`] returns them instead, so that it
//!   works on a shared (`&Connection`) connection as well.

use std::collections::HashMap;
use std::net::IpAddr;
use std::sync::{Arc, MutexGuard};

use async_trait::async_trait;
use hickory_proto::rr::rdata::svcb::{SVCB, SvcParamValue};
use hickory_proto::rr::{RData, Record};
use pm_endpoints::EpResult;
use pm_nameserver::handler::to_dns_request_context;
use pm_nameserver::{FilterOutcome, FirewallHook};
use pm_netstate::{IpScope, ip_scope};
use pm_network::{
    Connection, ConnectionType, DomainLookup, IpInfoNotFound, Network, OpenDnsQuery,
    ResolvedDomain as NetResolvedDomain, SharedConnection, Verdict,
};
use pm_profile::options::{
    CFG_OPTION_BLOCK_SCOPE_INTERNET_KEY, CFG_OPTION_BLOCK_SCOPE_LAN_KEY,
    CFG_OPTION_BLOCK_SCOPE_LOCAL_KEY, CFG_OPTION_FILTER_CNAME_KEY,
    CFG_OPTION_REMOVE_OUT_OF_SCOPE_DNS_KEY,
};
use pm_profile::{LayeredProfile, SYSTEM_RESOLVER_PROFILE_ID, UNIDENTIFIED_PROFILE_ID};
use pm_resolver::ipinfo::{
    DnsRequestContext, IP_INFO_PROFILE_SCOPE_GLOBAL, IpInfo, IpInfoError, IpInfoStore,
    ResolvedDomain,
};
use pm_resolver::model::SERVER_TYPE_ENV;
use pm_resolver::rr_text::{format_name, format_rr};
use pm_resolver::{Query, RRCache};

use crate::deciders::{NO_REASON_OPTION_KEY, check_connectivity_domain};
use crate::is_system_resolver;
use crate::master::{Decision, decide_on_connection};
use crate::packet::{LayeredProfileLookup, PacketEnv};

/// Ports of encrypted protocols (SSH, HTTPS, SMTPS, DoT, IMAPS, POP3S).
const ENCRYPTED_PORTS: [u16; 6] = [22, 443, 465, 853, 993, 995];

/// Go `formatRR`: the record text without tabs.
pub fn format_rr_text(rr: &Record) -> String {
    format_rr(rr)
        .unwrap_or_else(|| format!("{rr:?}"))
        .replace('\t', " ")
}

fn record_ip(rr: &Record) -> Option<IpAddr> {
    match &rr.data {
        RData::A(a) => Some(IpAddr::V4(a.0)),
        RData::AAAA(aaaa) => Some(IpAddr::V6(aaaa.0)),
        _ => None,
    }
}

/// The result of [`filter_dns_section`].
#[derive(Debug, Default)]
pub struct FilteredSection {
    pub good: Vec<Record>,
    pub filtered: Vec<String>,
    /// The number of valid and allowed A and AAAA records.
    pub allowed_address_records: usize,
    pub intervening_option_key: &'static str,
}

/// Go `filterDNSSection`.
pub fn filter_dns_section(
    entries: Vec<Record>,
    p: &LayeredProfile,
    resolver_scope: IpScope,
    sys_resolver: bool,
) -> FilteredSection {
    let mut out = FilteredSection {
        good: Vec::with_capacity(entries.len()),
        ..FilteredSection::default()
    };

    for rr in entries {
        // Add non A/AAAA entries.
        let Some(ip) = record_ip(&rr) else {
            out.good.push(rr);
            continue;
        };
        let scope = ip_scope(ip);

        if p.remove_out_of_scope_dns() {
            // No DNS should return localhost addresses, and no global DNS
            // should return LAN addresses.
            if scope.is_localhost()
                || (resolver_scope.is_global() && scope.is_lan() && !sys_resolver)
            {
                tracing::trace!(
                    "filter: RR violates resolver scope: {}",
                    format_rr_text(&rr)
                );
                out.filtered.push(format_rr_text(&rr));
                out.intervening_option_key = CFG_OPTION_REMOVE_OUT_OF_SCOPE_DNS_KEY;
                continue;
            }
        }

        if p.remove_blocked_dns() && !sys_resolver {
            let key = if p.block_scope_internet() && scope.is_global() {
                Some((CFG_OPTION_BLOCK_SCOPE_INTERNET_KEY, "Internet"))
            } else if p.block_scope_lan() && scope.is_lan() {
                Some((CFG_OPTION_BLOCK_SCOPE_LAN_KEY, "LAN"))
            } else if p.block_scope_local() && scope.is_localhost() {
                Some((CFG_OPTION_BLOCK_SCOPE_LOCAL_KEY, "Localhost"))
            } else {
                None
            };
            if let Some((key, name)) = key {
                tracing::trace!(
                    "filter: RR is in blocked scope {name}: {}",
                    format_rr_text(&rr)
                );
                out.filtered.push(format_rr_text(&rr));
                out.intervening_option_key = key;
                continue;
            }
        }

        // The record survived.
        out.allowed_address_records += 1;
        out.good.push(rr);
    }

    out
}

/// Go `filterDNSResponse`.
pub fn filter_dns_response(
    conn: &mut Connection,
    p: &LayeredProfile,
    mut rr_cache: RRCache,
    sys_resolver: bool,
    own_pid: i32,
) -> RRCache {
    // Do not modify own queries.
    if conn.process.as_ref().map(|process| process.pid) == Some(own_pid) {
        return rr_cache;
    }

    // Check if DNS response filtering is completely turned off.
    if !p.remove_out_of_scope_dns() && !p.remove_blocked_dns() {
        return rr_cache;
    }

    let resolver_scope = rr_cache
        .resolver
        .as_ref()
        .map(|r| r.ip_scope)
        .unwrap_or_default();

    let section = filter_dns_section(
        std::mem::take(&mut rr_cache.answer),
        p,
        resolver_scope,
        sys_resolver,
    );
    rr_cache.answer = section.good;
    rr_cache.filtered_entries.extend(section.filtered);
    let valid_ips = section.allowed_address_records;
    let intervening_option_key = section.intervening_option_key;

    // Don't count the valid IPs in the extra section.
    let extra = filter_dns_section(
        std::mem::take(&mut rr_cache.extra),
        p,
        resolver_scope,
        sys_resolver,
    );
    rr_cache.extra = extra.good;
    rr_cache.filtered_entries.extend(extra.filtered);

    if !rr_cache.filtered_entries.is_empty() {
        rr_cache.filtered = true;
        if valid_ips == 0 {
            let reason = match intervening_option_key {
                CFG_OPTION_BLOCK_SCOPE_INTERNET_KEY => "Internet access blocked",
                CFG_OPTION_BLOCK_SCOPE_LAN_KEY => "LAN access blocked",
                CFG_OPTION_BLOCK_SCOPE_LOCAL_KEY => "Localhost access blocked",
                CFG_OPTION_REMOVE_OUT_OF_SCOPE_DNS_KEY => "DNS global/private split-view violation",
                _ => "DNS response only contained to-be-blocked IPs",
            };
            conn.block(reason, intervening_option_key);
        }
    }

    rr_cache
}

/// Go `mayBlockCNAMEs`: re-checks the lists and endpoints with the CNAMEs.
pub fn may_block_cnames(conn: &mut Connection, p: &LayeredProfile) -> bool {
    // If we have CNAMEs and the profile is configured to filter them, the
    // lists and endpoints need to be checked again.
    if !p.filter_cnames() {
        return false;
    }
    let Some(entity) = conn.entity.as_mut() else {
        return false;
    };
    entity.reset_lists();
    entity.enable_cname_check(true);

    let (result, reason) = p.match_endpoint(entity);
    if result == EpResult::Denied {
        conn.set_verdict(
            Verdict::Block,
            &reason.as_ref().map(ToString::to_string).unwrap_or_default(),
            CFG_OPTION_FILTER_CNAME_KEY,
            reason.as_ref().and_then(|r| serde_json::to_value(r).ok()),
        );
        return true;
    }

    if result == EpResult::NoMatch {
        let (result, reason) = p.match_filter_lists(entity);
        if result == EpResult::Denied {
            let reason = reason.unwrap_or_default();
            conn.set_verdict(
                Verdict::Block,
                &reason.to_string(),
                CFG_OPTION_FILTER_CNAME_KEY,
                Some(reason.to_json()),
            );
            return true;
        }
    }

    false
}

/// Go `FilterResolvedDNS`: filters a DNS response according to the profile
/// and settings. Returns `None` if the process or profile is unknown.
pub fn filter_resolved_dns(
    conn: &mut Connection,
    profile: Option<&LayeredProfile>,
    rr_cache: RRCache,
    env: &dyn PacketEnv,
) -> Option<RRCache> {
    // Check if we have a process and profile.
    let Some(profile) = profile else {
        tracing::warn!("unknown process or profile");
        return None;
    };

    // Don't filter env responses.
    if rr_cache
        .resolver
        .as_ref()
        .is_some_and(|r| r.r#type == SERVER_TYPE_ENV)
    {
        return Some(rr_cache);
    }

    // Special grant for connectivity domains.
    if check_connectivity_domain(conn, profile, env, &mut Decision::default()) {
        return Some(rr_cache);
    }

    // Only filter critical things if the request comes from the system resolver.
    let sys_resolver = conn.process.as_deref().is_some_and(is_system_resolver);

    // Filter dns records and return if the query is blocked.
    let rr_cache = filter_dns_response(conn, profile, rr_cache, sys_resolver, env.own_pid());
    if conn.verdict == Verdict::Block {
        return Some(rr_cache);
    }

    // Block by CNAMEs.
    if !sys_resolver {
        may_block_cnames(conn, profile);
    }

    Some(rr_cache)
}

fn svcb_hints(svcb: &SVCB, ips: &mut Vec<IpAddr>) {
    for (_, value) in &svcb.svc_params {
        match value {
            SvcParamValue::Ipv4Hint(hint) => ips.extend(hint.0.iter().map(|a| IpAddr::V4(a.0))),
            SvcParamValue::Ipv6Hint(hint) => ips.extend(hint.0.iter().map(|a| IpAddr::V6(a.0))),
            _ => {}
        }
    }
}

fn svcb_target(rr: &Record, svcb: &SVCB, cnames: &mut HashMap<String, String>) {
    // Ignore "" and ".".
    let target = format_name(&svcb.target_name);
    if target.len() >= 2 {
        cnames.insert(format_name(&rr.name), target);
    }
}

/// Go `UpdateIPsAndCNAMEs`, without the connection: collects the IPs and the
/// resolved domain of the response.
pub fn collect_ips_and_cnames(q: &Query, rr: &RRCache) -> (Vec<IpAddr>, ResolvedDomain) {
    let mut cnames = HashMap::new();
    let mut ips = Vec::with_capacity(rr.answer.len());

    for record in rr.answer.iter().chain(rr.extra.iter()) {
        match &record.data {
            RData::CNAME(target) => {
                cnames.insert(format_name(&record.name), format_name(&target.0));
            }
            RData::A(a) => ips.push(IpAddr::V4(a.0)),
            RData::AAAA(aaaa) => ips.push(IpAddr::V6(aaaa.0)),
            RData::SVCB(svcb) => {
                svcb_target(record, svcb, &mut cnames);
                svcb_hints(svcb, &mut ips);
            }
            RData::HTTPS(https) => {
                svcb_target(record, &https.0, &mut cnames);
                svcb_hints(&https.0, &mut ips);
            }
            _ => {}
        }
    }

    // Create the new record for the IPs.
    let ctx = to_dns_request_context(rr);
    let mut resolved = ResolvedDomain {
        domain: q.fqdn.clone(),
        cnames: None,
        resolver: rr.resolver.clone(),
        dns_request_context: Some(DnsRequestContext {
            domain: ctx.domain,
            question: ctx.question,
            rcode: ctx.rcode,
            served_from_cache: ctx.served_from_cache,
            requesting_new: ctx.requesting_new,
            is_backup: ctx.is_backup,
            filtered: ctx.filtered,
            modified: ctx.modified,
            expires: ctx.expires,
        }),
        expires: rr.expires,
    };
    resolved.add_cnames(&cnames);
    (ips, resolved)
}

/// Go `SaveIPsInCache`: saves the IPs with the domain and CNAMEs of the record.
pub fn save_ips_in_cache(
    store: &IpInfoStore,
    ips: &[IpAddr],
    profile_id: &str,
    record: &ResolvedDomain,
) {
    for ip in ips {
        // Never save domain attributions for localhost IPs.
        if ip_scope(*ip) == IpScope::HostLocal {
            continue;
        }

        let ip_string = ip.to_string();
        let mut info = match store.get(profile_id, &ip_string) {
            Ok(info) => info,
            Err(err) => {
                if !matches!(err, IpInfoError::NotFound) {
                    tracing::error!("nameserver: failed to search for IP info record: {err}");
                }
                IpInfo::new(profile_id, &ip_string)
            }
        };

        // Add the new record to the resolved domains for this IP and scope.
        info.add_domain(record.clone());

        // Save if the record is new or has been updated.
        if let Err(err) = store.save(&mut info) {
            tracing::error!("nameserver: failed to save IP info record: {err}");
        }
    }
}

/// The profile scope of IP info for the local profile ID.
pub fn ip_info_profile_scope(local_profile_id: &str) -> &str {
    match local_profile_id {
        UNIDENTIFIED_PROFILE_ID | SYSTEM_RESOLVER_PROFILE_ID => IP_INFO_PROFILE_SCOPE_GLOBAL,
        id => id,
    }
}

/// Go `UpdateIPsAndCNAMEs`: saves all IP to name mappings to the cache
/// database. Returns the CNAMEs that have to be set on the entity of a DNS
/// request connection.
pub fn update_ips_and_cnames(
    store: &IpInfoStore,
    profile: Option<&LayeredProfile>,
    q: &Query,
    rr: &RRCache,
    conn: &Connection,
) -> Option<Vec<String>> {
    let profile_id = profile
        .map(|p| ip_info_profile_scope(&p.local_profile().id).to_string())
        .unwrap_or_else(|| IP_INFO_PROFILE_SCOPE_GLOBAL.to_string());
    let (ips, resolved) = collect_ips_and_cnames(q, rr);
    save_ips_in_cache(store, &ips, &profile_id, &resolved);
    (conn.conn_type == ConnectionType::Dns).then(|| resolved.cnames.unwrap_or_default())
}

/// [`DomainLookup`] on top of the IP info store.
pub struct IpInfoDomains {
    pub store: Arc<IpInfoStore>,
}

impl DomainLookup for IpInfoDomains {
    fn most_recent_domain(
        &self,
        profile_scope: &str,
        ip: IpAddr,
    ) -> Result<Option<NetResolvedDomain>, IpInfoNotFound> {
        let info = self
            .store
            .get(profile_scope, &ip.to_string())
            .map_err(|_| IpInfoNotFound)?;
        Ok(info.most_recent_domain().map(|resolved| NetResolvedDomain {
            domain: resolved.domain.clone(),
            cnames: resolved.cnames.clone().unwrap_or_default(),
            dns_context: resolved.dns_request_context.as_ref().map(|c| {
                pm_network::DnsRequestContext {
                    domain: c.domain.clone(),
                    question: c.question.clone(),
                    rcode: c.rcode.clone(),
                    served_from_cache: c.served_from_cache,
                    requesting_new: c.requesting_new,
                    is_backup: c.is_backup,
                    filtered: c.filtered,
                    modified: c.modified.clone(),
                    expires: c.expires.clone(),
                }
            }),
            resolver: resolved.resolver.clone(),
        }))
    }
}

/// The firewall step of the nameserver.
pub struct FirewallDnsHook {
    env: Arc<dyn PacketEnv>,
    profiles: Arc<dyn LayeredProfileLookup>,
    ip_info: Arc<IpInfoStore>,
    network: Option<Arc<Network>>,
}

fn lock(conn: &SharedConnection) -> MutexGuard<'_, Connection> {
    conn.lock().unwrap_or_else(|e| e.into_inner())
}

impl FirewallDnsHook {
    /// Creates the hook. Requests are saved through the network when given.
    pub fn new(
        env: Arc<dyn PacketEnv>,
        profiles: Arc<dyn LayeredProfileLookup>,
        ip_info: Arc<IpInfoStore>,
        network: Option<Arc<Network>>,
    ) -> FirewallDnsHook {
        FirewallDnsHook {
            env,
            profiles,
            ip_info,
            network,
        }
    }

    fn profile_of(&self, conn: &Connection) -> Option<Arc<LayeredProfile>> {
        conn.process
            .as_ref()
            .and_then(|process| self.profiles.layered_profile(process))
    }

    /// Go `FilterConnection(ctx, conn, nil, true, false)`.
    fn filter(&self, conn: &mut Connection) -> Decision {
        let mut decision = Decision::default();

        // Skip if data is not complete.
        if !conn.data_complete {
            return decision;
        }

        // Location data is always fetched for the UI.
        if let Some(entity) = conn.entity.as_mut() {
            entity.fetch_location();
        }

        // The external verdict handler can override the filter flag.
        let mut check_filter = true;
        if let Some(external) = self.env.external_verdict(conn) {
            if matches!(external.verdict, Verdict::Accept | Verdict::Block) {
                conn.set_verdict(external.verdict, &external.reason, "", None);
                check_filter = false;
            }
        }

        if check_filter {
            if self.env.filter_enabled() {
                let profile = self.profile_of(conn);
                decision = decide_on_connection(conn, profile.as_deref(), self.env.as_ref());
            } else {
                conn.accept("privacy filter disabled", NO_REASON_OPTION_KEY);
            }
        }

        if conn
            .entity
            .as_ref()
            .is_some_and(|entity| ENCRYPTED_PORTS.contains(&entity.dst_port()))
        {
            conn.encrypted = true;
        }
        decision
    }
}

#[async_trait]
impl FirewallHook for FirewallDnsHook {
    async fn filter_connection(&self, conn: &SharedConnection) -> FilterOutcome {
        let mut guard = lock(conn);
        self.filter(&mut guard).into_filter_outcome()
    }

    async fn filter_resolved_dns(
        &self,
        conn: &SharedConnection,
        _q: &Query,
        rr_cache: RRCache,
    ) -> (Option<RRCache>, FilterOutcome) {
        let mut guard = lock(conn);
        let profile = self.profile_of(&guard);
        let result =
            filter_resolved_dns(&mut guard, profile.as_deref(), rr_cache, self.env.as_ref());
        (result, FilterOutcome::default())
    }

    fn update_ips_and_cnames(&self, q: &Query, rr: &RRCache, conn: &SharedConnection) {
        let mut guard = lock(conn);
        let profile = self.profile_of(&guard);
        if let Some(cnames) =
            update_ips_and_cnames(&self.ip_info, profile.as_deref(), q, rr, &guard)
        {
            if let Some(entity) = guard.entity.as_mut() {
                entity.cname = cnames;
            }
        }
    }

    fn save_open_dns_request(&self, q: &Query, rr: &RRCache, conn: &SharedConnection) {
        if let Some(network) = &self.network {
            let query = OpenDnsQuery {
                qtype: u16::from(q.qtype),
                served_from_cache: rr.served_from_cache,
                requesting_new: rr.requesting_new,
            };
            network.save_open_dns_request(Some(&query), conn);
        }
    }

    fn save_connection(&self, conn: &SharedConnection) {
        if let Some(network) = &self.network {
            network.save(conn);
        }
    }
}
