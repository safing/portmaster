//! Port of Go `rrcache.go`: the in-memory DNS response with its caching rules.

use std::net::IpAddr;
use std::time::{SystemTime, UNIX_EPOCH};

use hickory_proto::op::Message;
use hickory_proto::rr::{DNSClass, RData, Record, RecordType};

use crate::model::ResolverInfo;
use crate::namerecord::NameRecord;
use crate::rr_text::{format_rr, is_persistable, parse_rr};

/// Minimum TTL of cached records in seconds.
pub const MIN_TTL: u32 = 60;
/// Entries that expire within this many seconds are refreshed in the background.
pub const REFRESH_TTL: i64 = (MIN_TTL / 2) as i64;
/// Maximum TTL of cached records in seconds (24 hours).
pub const MAX_TTL: u32 = 24 * 60 * 60;

/// TTL written into all records handed to clients.
pub const CLIENT_TTL: u32 = 17;

pub const RCODE_SUCCESS: u16 = 0;
pub const RCODE_NAME_ERROR: u16 = 3;
pub const RCODE_REFUSED: u16 = 5;

/// Returns the current Unix time in seconds.
pub fn now_unix() -> i64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|d| d.as_secs() as i64)
        .unwrap_or(0)
}

/// Returns the name of a query type like Go `dns.Type.String()`.
pub fn qtype_string(qtype: RecordType) -> String {
    let name = qtype.to_string();
    if name.starts_with("Unknown") || name == "ZERO" || name == "ANAME" {
        format!("TYPE{}", u16::from(qtype))
    } else {
        name
    }
}

/// Environment facts that influence how long a response is cached.
#[derive(Debug, Clone, Copy, Default)]
pub struct CleanContext {
    /// The domain is a connectivity check domain.
    pub connectivity_domain: bool,
    /// The device is fully online.
    pub online: bool,
}

/// A single-use structure to hold a DNS response.
#[derive(Debug, Clone)]
pub struct RRCache {
    pub domain: String,
    pub question: RecordType,
    pub rcode: u16,

    pub answer: Vec<Record>,
    pub ns: Vec<Record>,
    pub extra: Vec<Record>,
    pub expires: i64,

    pub resolver: Option<ResolverInfo>,

    pub served_from_cache: bool,
    pub requesting_new: bool,
    pub is_backup: bool,
    pub filtered: bool,
    pub filtered_entries: Vec<String>,

    /// When this entry was last saved; only set for entries from the cache.
    pub modified: i64,
}

impl RRCache {
    /// Creates a cache entry from a resolver reply.
    pub fn from_reply(
        domain: &str,
        question: RecordType,
        reply: Message,
        resolver: &ResolverInfo,
    ) -> RRCache {
        RRCache {
            domain: domain.to_string(),
            question,
            rcode: u16::from(reply.metadata.response_code),
            answer: reply.answers,
            ns: reply.authorities,
            extra: reply.additionals,
            expires: 0,
            resolver: Some(resolver.clone()),
            served_from_cache: false,
            requesting_new: false,
            is_backup: false,
            filtered: false,
            filtered_entries: Vec::new(),
            modified: 0,
        }
    }

    /// Returns the ID of the entry: domain and question type.
    pub fn id(&self) -> String {
        format!("{}{}", self.domain, qtype_string(self.question))
    }

    pub fn expired(&self) -> bool {
        self.expires <= now_unix()
    }

    /// Returns whether the entry expires soon (or already did).
    pub fn expires_soon(&self) -> bool {
        self.expires <= now_unix() + REFRESH_TTL
    }

    /// Sets all TTLs to 17 and the cache expiry, with the given minimum.
    pub fn clean(&mut self, min_expires: u32, ctx: CleanContext) {
        let mut lowest_ttl = u32::MAX;
        for record in self
            .answer
            .iter_mut()
            .chain(self.ns.iter_mut())
            .chain(self.extra.iter_mut())
        {
            lowest_ttl = lowest_ttl.min(record.ttl);
            record.ttl = CLIENT_TTL;
        }

        // TTL range limits.
        if lowest_ttl < min_expires {
            lowest_ttl = min_expires;
        } else if lowest_ttl > MAX_TTL {
            lowest_ttl = MAX_TTL;
        }

        // Shorten caching.
        if self.rcode != RCODE_SUCCESS {
            // Any sort of error.
            lowest_ttl = 10;
        } else if ctx.connectivity_domain {
            // Responses from these domains might change very quickly.
            lowest_ttl = 3;
        } else if self.answer.is_empty() {
            // Domain exists, but not the queried RR.
            lowest_ttl = 60;
        } else if !ctx.online {
            // Not being fully online could mean that we get funny responses.
            lowest_ttl = 60;
        }

        self.expires = now_unix() + i64::from(lowest_ttl);
    }

    /// Returns all A and AAAA addresses of the answer section.
    pub fn export_all_a_records(&self) -> Vec<IpAddr> {
        self.answer
            .iter()
            .filter(|r| r.dns_class == DNSClass::IN)
            .filter_map(|r| match &r.data {
                RData::A(a) => Some(IpAddr::V4(a.0)),
                RData::AAAA(aaaa) => Some(IpAddr::V6(aaaa.0)),
                _ => None,
            })
            .collect()
    }

    /// Returns whether a record with the given response code should be cached.
    pub fn rcode_is_cacheable(rcode: u16) -> bool {
        matches!(rcode, RCODE_SUCCESS | RCODE_NAME_ERROR | RCODE_REFUSED)
    }

    /// Returns whether the record should be cached.
    pub fn cacheable(&self) -> bool {
        Self::rcode_is_cacheable(self.rcode)
    }

    /// Converts the entry to a name record for persistence. Fails if a record
    /// cannot be serialized, as a partial answer must not be cached.
    pub fn to_name_record(&self) -> Result<NameRecord, String> {
        let section = |records: &[Record]| -> Result<Vec<String>, String> {
            records
                .iter()
                .filter(|r| is_persistable(r))
                .map(|r| format_rr(r).ok_or_else(|| "failed to serialize record".to_string()))
                .collect()
        };
        Ok(NameRecord {
            domain: self.domain.clone(),
            question: qtype_string(self.question),
            rcode: i64::from(self.rcode),
            answer: section(&self.answer)?,
            ns: section(&self.ns)?,
            extra: section(&self.extra)?,
            expires: self.expires,
            resolver: self.resolver.clone(),
        })
    }

    /// Rebuilds an entry from a name record. Unparseable records fail the
    /// whole conversion, so a partial answer is never served.
    pub fn from_name_record(
        record: &NameRecord,
        question: RecordType,
        modified: i64,
    ) -> Result<RRCache, String> {
        let parse = |entries: &[String]| -> Result<Vec<Record>, String> {
            entries
                .iter()
                .map(|e| {
                    parse_rr(e).map_err(|err| format!("failed to parse cached record {e:?}: {err}"))
                })
                .collect()
        };
        Ok(RRCache {
            domain: record.domain.clone(),
            question,
            rcode: u16::try_from(record.rcode).map_err(|_| "invalid cached rcode".to_string())?,
            answer: parse(&record.answer)?,
            ns: parse(&record.ns)?,
            extra: parse(&record.extra)?,
            expires: record.expires,
            resolver: record.resolver.clone(),
            served_from_cache: true,
            requesting_new: false,
            is_backup: false,
            filtered: false,
            filtered_entries: Vec::new(),
            modified,
        })
    }

    /// Formats the cache flags like Go `Flags`.
    pub fn flags(&self) -> String {
        let mut s = String::new();
        if self.served_from_cache {
            s.push('C');
        }
        if self.requesting_new {
            s.push('R');
        }
        if self.is_backup {
            s.push('B');
        }
        if self.filtered {
            s.push('F');
        }
        if s.is_empty() {
            s
        } else {
            format!(" [{s}]")
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use hickory_proto::rr::rdata::A;
    use hickory_proto::rr::Name;
    use std::net::Ipv4Addr;
    use std::str::FromStr;

    fn a_record(ttl: u32) -> Record {
        Record::from_rdata(
            Name::from_str("example.com.").unwrap(),
            ttl,
            RData::A(A(Ipv4Addr::new(192, 0, 2, 1))),
        )
    }

    fn cache(rcode: u16, answers: Vec<Record>) -> RRCache {
        RRCache {
            domain: "example.com.".into(),
            question: RecordType::A,
            rcode,
            answer: answers,
            ns: vec![],
            extra: vec![],
            expires: 0,
            resolver: None,
            served_from_cache: false,
            requesting_new: false,
            is_backup: false,
            filtered: false,
            filtered_entries: vec![],
            modified: 0,
        }
    }

    const ONLINE: CleanContext = CleanContext {
        connectivity_domain: false,
        online: true,
    };

    fn ttl_of(c: &RRCache) -> i64 {
        c.expires - now_unix()
    }

    fn assert_ttl(c: &RRCache, want: i64) {
        let got = ttl_of(c);
        assert!((want - 1..=want).contains(&got), "ttl {got}, want {want}");
    }

    #[test]
    fn clean_clamps_ttl() {
        let mut c = cache(0, vec![a_record(5)]);
        c.clean(MIN_TTL, ONLINE);
        assert_ttl(&c, 60);
        assert_eq!(c.answer[0].ttl, 17);

        let mut c = cache(0, vec![a_record(300), a_record(120)]);
        c.clean(MIN_TTL, ONLINE);
        assert_ttl(&c, 120);

        let mut c = cache(0, vec![a_record(10 * 86400)]);
        c.clean(MIN_TTL, ONLINE);
        assert_ttl(&c, 86400);
    }

    #[test]
    fn clean_shortens_special_cases() {
        let mut c = cache(RCODE_NAME_ERROR, vec![]);
        c.clean(MIN_TTL, ONLINE);
        assert_ttl(&c, 10);

        let mut c = cache(0, vec![a_record(300)]);
        c.clean(
            MIN_TTL,
            CleanContext {
                connectivity_domain: true,
                online: true,
            },
        );
        assert_ttl(&c, 3);

        // Empty answer section.
        let mut c = cache(0, vec![]);
        c.clean(MIN_TTL, ONLINE);
        assert_ttl(&c, 60);

        // Not online.
        let mut c = cache(0, vec![a_record(300)]);
        c.clean(MIN_TTL, CleanContext::default());
        assert_ttl(&c, 60);

        // Errors win over the connectivity domain rule.
        let mut c = cache(2, vec![]);
        c.clean(
            MIN_TTL,
            CleanContext {
                connectivity_domain: true,
                online: true,
            },
        );
        assert_ttl(&c, 10);
    }

    #[test]
    fn cacheable_rcodes() {
        for (rcode, want) in [
            (0, true),
            (3, true),
            (5, true),
            (1, false),
            (2, false),
            (4, false),
        ] {
            assert_eq!(cache(rcode, vec![]).cacheable(), want, "{rcode}");
        }
    }

    #[test]
    fn expiry_checks() {
        let mut c = cache(0, vec![]);
        c.expires = now_unix() - 1;
        assert!(c.expired());
        c.expires = now_unix() + 10;
        assert!(!c.expired());
        assert!(c.expires_soon());
        c.expires = now_unix() + 120;
        assert!(!c.expires_soon());
    }

    #[test]
    fn name_record_roundtrip() {
        let mut c = cache(0, vec![a_record(17)]);
        c.expires = 1_700_000_060;
        let nr = c.to_name_record().unwrap();
        assert_eq!(nr.question, "A");
        assert_eq!(
            nr.answer,
            vec!["example.com.\t17\tIN\tA\t192.0.2.1".to_string()]
        );
        let back = RRCache::from_name_record(&nr, RecordType::A, 5).unwrap();
        assert_eq!(back.answer, c.answer);
        assert!(back.served_from_cache);
        assert_eq!(back.modified, 5);
    }

    #[test]
    fn qtype_names() {
        assert_eq!(qtype_string(RecordType::A), "A");
        assert_eq!(qtype_string(RecordType::AAAA), "AAAA");
        assert_eq!(qtype_string(RecordType::HTTPS), "HTTPS");
        assert_eq!(qtype_string(RecordType::Unknown(999)), "TYPE999");
    }
}
