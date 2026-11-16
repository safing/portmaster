//! Port of Go `inspectDNSPacket`: observes DNS packets of connections that
//! bypass the Portmaster resolver and feeds the answers to the IP info cache.

use hickory_proto::op::Message;
use pm_interception::Verdict as PktVerdict;
use pm_network::{Connection, SharedConnection, Verdict};
use pm_resolver::model::{SERVER_SOURCE_FIREWALL, SERVER_TYPE_FIREWALL};
use pm_resolver::scopes::INTERNAL_SPECIAL_USE_DOMAIN;
use pm_resolver::{Query, RRCache, ResolverInfo};

use super::handler::PacketHandler;
use super::queued::QueuedPacket;

impl PacketHandler {
    pub(super) fn inspect_dns_packet(
        &self,
        shared: &SharedConnection,
        conn: &mut Connection,
        pkt: &dyn QueuedPacket,
    ) {
        // Ignore info-only packets in this handler.
        if pkt.info_only() {
            return;
        }

        let Some(payload) = pkt.payload() else {
            let _ = pkt.set_verdict(PktVerdict::Block);
            tracing::error!("filter: failed to load packet payload");
            return;
        };

        // Parse and block invalid packets.
        let Ok(message) = Message::from_vec(payload) else {
            if let Err(error) = pkt.set_verdict(PktVerdict::PermanentBlock) {
                tracing::error!("filter: failed to block packet: {error}");
            }
            conn.set_verdict(Verdict::Block, "none DNS data on DNS port", "", None);
            conn.verdict_permanent = true;
            self.network.save_locked(shared, conn);
            return;
        };

        // The packet was parsed: allow it, but only after the answer was added
        // to the cache.
        self.observe_dns(conn, message);
        if let Err(error) = pkt.set_verdict(PktVerdict::Accept) {
            tracing::error!("filter: failed to accept dns packet: {error}");
        }
    }

    fn observe_dns(&self, conn: &Connection, message: Message) {
        // Check if the packet has a question.
        let Some(question) = message.queries.first() else {
            return;
        };
        let mut fqdn = question.name().to_ascii();
        if !fqdn.ends_with('.') {
            fqdn.push('.');
        }
        let qtype = question.query_type();

        // Check for the compat check DNS request.
        let check_scope = format!(".self-check.{INTERNAL_SPECIAL_USE_DOMAIN}");
        if let Some(subdomain) = fqdn.strip_suffix(&check_scope) {
            self.env.submit_dns_check_domain(subdomain);
            tracing::info!("packet_handler: self-check domain received");
            // No need to parse the answer.
            return;
        }

        // Check if there is an answer.
        if message.answers.is_empty() {
            return;
        }

        let resolver = conn.entity.as_ref().map(|entity| ResolverInfo {
            name: "DNSRequestObserver".to_string(),
            r#type: SERVER_TYPE_FIREWALL.to_string(),
            source: SERVER_SOURCE_FIREWALL.to_string(),
            ip: entity.ip,
            domain: entity.domain.clone(),
            ip_scope: entity.ip_scope,
            port: 0,
        });
        let rr_cache = RRCache {
            domain: fqdn.clone(),
            question: qtype,
            rcode: u16::from(message.metadata.response_code),
            answer: message.answers,
            ns: message.authorities,
            extra: message.additionals,
            expires: 0,
            resolver,
            served_from_cache: false,
            requesting_new: false,
            is_backup: false,
            filtered: false,
            filtered_entries: Vec::new(),
            modified: 0,
        };
        let query = Query::new(fqdn, qtype);

        // Save to the cache.
        self.env.update_ips_and_cnames(&query, &rr_cache, conn);
    }
}
