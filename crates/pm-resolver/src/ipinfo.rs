//! Port of Go `ipinfo.go`: which domains resolved to an IP, persisted in the
//! cache database under `cache:intel/ipInfo/<profile>/<ip>`.

use std::collections::HashMap;
use std::fmt;

use pm_database::{Interface, Options as DbOptions};
use pm_record::{Key, Record};
use serde::{Deserialize, Deserializer, Serialize};

use crate::model::ResolverInfo;
use crate::rrcache::now_unix;

/// Profile scope used for unscoped IP info entries.
pub const IP_INFO_PROFILE_SCOPE_GLOBAL: &str = "global";

/// Errors of the IP info store.
#[derive(Debug, thiserror::Error)]
pub enum IpInfoError {
    #[error("database entry not found")]
    NotFound,
    #[error("{0}")]
    Other(String),
}

impl From<pm_database::Error> for IpInfoError {
    fn from(err: pm_database::Error) -> Self {
        match err {
            pm_database::Error::NotFound => IpInfoError::NotFound,
            other => IpInfoError::Other(other.to_string()),
        }
    }
}

fn null_as_empty<'de, D: Deserializer<'de>>(d: D) -> Result<Vec<ResolvedDomain>, D::Error> {
    Ok(Option::<Vec<ResolvedDomain>>::deserialize(d)?.unwrap_or_default())
}

/// Go `resolver.DNSRequestContext`; the timestamps are RFC 3339 strings.
#[derive(Debug, Clone, Default, PartialEq, Eq, Serialize, Deserialize)]
pub struct DnsRequestContext {
    #[serde(rename = "Domain", default)]
    pub domain: String,
    #[serde(rename = "Question", default)]
    pub question: String,
    #[serde(rename = "RCode", default)]
    pub rcode: String,
    #[serde(rename = "ServedFromCache", default)]
    pub served_from_cache: bool,
    #[serde(rename = "RequestingNew", default)]
    pub requesting_new: bool,
    #[serde(rename = "IsBackup", default)]
    pub is_backup: bool,
    #[serde(rename = "Filtered", default)]
    pub filtered: bool,
    #[serde(rename = "Modified", default)]
    pub modified: String,
    #[serde(rename = "Expires", default)]
    pub expires: String,
}

/// A domain name with the CNAMEs that were resolved for it.
#[derive(Debug, Clone, Default, PartialEq, Eq, Serialize, Deserialize)]
pub struct ResolvedDomain {
    /// The domain as requested by the application.
    #[serde(rename = "Domain", default)]
    pub domain: String,
    /// `None` is Go's nil slice (JSON `null`).
    #[serde(rename = "CNAMEs", default)]
    pub cnames: Option<Vec<String>>,
    #[serde(rename = "Resolver", default)]
    pub resolver: Option<ResolverInfo>,
    #[serde(rename = "DNSRequestContext", default)]
    pub dns_request_context: Option<DnsRequestContext>,
    /// When the entry expires; only used to compute the record TTL.
    #[serde(rename = "Expires", default)]
    pub expires: i64,
}

impl ResolvedDomain {
    /// Adds all CNAMEs from the map that are related to the domain, following
    /// the chain for up to 50 layers.
    pub fn add_cnames(&mut self, cnames: &HashMap<String, String>) {
        let mut domain = self.domain.clone();
        for _ in 0..50 {
            let Some(next) = cnames.get(&domain) else {
                break;
            };
            if *next == self.domain || *next == domain {
                break;
            }
            self.cnames.get_or_insert_with(Vec::new).push(next.clone());
            domain = next.clone();
        }
    }
}

impl fmt::Display for ResolvedDomain {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(&self.domain)?;
        if let Some(cnames) = self.cnames.as_ref().filter(|c| !c.is_empty()) {
            write!(f, " (-> {})", cnames.join("->"))?;
        }
        Ok(())
    }
}

/// Information about an IP: the domains that resolved to it.
#[derive(Debug, Clone, Default, PartialEq, Eq, Serialize, Deserialize)]
pub struct IpInfo {
    #[serde(rename = "IP", default)]
    pub ip: String,
    /// Scopes this entry to a process group.
    #[serde(rename = "ProfileID", default)]
    pub profile_id: String,
    #[serde(
        rename = "ResolvedDomains",
        default,
        deserialize_with = "null_as_empty"
    )]
    pub resolved_domains: Vec<ResolvedDomain>,
}

impl IpInfo {
    pub fn new(profile_id: &str, ip: &str) -> IpInfo {
        IpInfo {
            ip: ip.to_string(),
            profile_id: profile_id.to_string(),
            resolved_domains: Vec::new(),
        }
    }

    /// Adds a resolved domain at the end, replacing an older entry of the same domain.
    pub fn add_domain(&mut self, resolved: ResolvedDomain) {
        if let Some(idx) = self
            .resolved_domains
            .iter()
            .position(|d| d.domain == resolved.domain)
        {
            self.resolved_domains.remove(idx);
        }
        self.resolved_domains.push(resolved);
    }

    /// Returns the most recently added domain.
    pub fn most_recent_domain(&self) -> Option<&ResolvedDomain> {
        self.resolved_domains.last()
    }

    /// Returns the database key of the record.
    pub fn key(&self) -> String {
        make_ip_info_key(&self.profile_id, &self.ip)
    }

    /// Serializes the record like Go `json.Marshal`.
    pub fn to_json(&self) -> Result<Vec<u8>, IpInfoError> {
        pm_dsd::gojson::to_vec(self).map_err(|e| IpInfoError::Other(e.to_string()))
    }
}

impl fmt::Display for IpInfo {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        let domains: Vec<String> = self
            .resolved_domains
            .iter()
            .map(ToString::to_string)
            .collect();
        write!(
            f,
            "<IPInfo[{}] {}: {}>",
            self.key(),
            self.ip,
            domains.join(" or ")
        )
    }
}

/// Returns the database key of an IP info record.
pub fn make_ip_info_key(profile_id: &str, ip: &str) -> String {
    format!("cache:intel/ipInfo/{profile_id}/{ip}")
}

/// Access to the IP info records in the cache database.
pub struct IpInfoStore {
    db: Interface,
}

impl IpInfoStore {
    /// Creates a store on top of a database interface; use [`ip_info_interface_options`].
    pub fn new(db: Interface) -> IpInfoStore {
        IpInfoStore { db }
    }

    /// Gets an IP info record.
    pub fn get(&self, profile_id: &str, ip: &str) -> Result<IpInfo, IpInfoError> {
        let record = self.db.get(&make_ip_info_key(profile_id, ip))?;
        record
            .decode()
            .map_err(|e| IpInfoError::Other(e.to_string()))
    }

    /// Saves the record.
    pub fn save(&self, info: &mut IpInfo) -> Result<(), IpInfoError> {
        self.save_at(info, now_unix())
    }

    /// Saves the record, with `now` as the current time. The expiry is the
    /// latest domain expiry (at least one day away) plus one hour.
    pub fn save_at(&self, info: &mut IpInfo, now: i64) -> Result<(), IpInfoError> {
        if info.profile_id.is_empty() {
            info.profile_id = IP_INFO_PROFILE_SCOPE_GLOBAL.to_string();
        }
        let key = info.key();

        let mut expires = now + 86400;
        for resolved in &info.resolved_domains {
            expires = expires.max(resolved.expires);
        }
        expires += 3600;

        // Keep the creation time of an existing record.
        let mut meta = self.db.get_meta(&key).unwrap_or_default();
        meta.set_absolute_expiry(expires);
        let record = Record {
            key: Key::parse(&key),
            meta,
            format: pm_dsd::format::JSON,
            data: info.to_json()?,
        };
        Ok(self.db.put(record)?)
    }
}

/// Database interface options of the IP info store: local and internal.
pub fn ip_info_interface_options() -> DbOptions {
    crate::namerecord::record_interface_options()
}

#[cfg(test)]
mod tests {
    use super::*;

    fn rd(domain: &str, cnames: &[&str]) -> ResolvedDomain {
        ResolvedDomain {
            domain: domain.into(),
            cnames: Some(cnames.iter().map(|c| (*c).to_string()).collect()),
            ..ResolvedDomain::default()
        }
    }

    #[test]
    fn add_domain_matches_go_test() {
        let example = rd("example.com.", &[]);
        let sub_example = rd("sub1.example.com", &["example.com"]);
        let mut info = IpInfo::new("", "1.2.3.4");
        info.resolved_domains = vec![example.clone(), sub_example.clone()];

        let sub2 = rd("sub2.example.com", &["sub1.example.com", "example.com"]);
        info.add_domain(sub2.clone());
        assert_eq!(
            info.resolved_domains,
            [example.clone(), sub_example, sub2.clone()]
        );
        info.add_domain(sub2.clone());
        assert_eq!(info.resolved_domains.len(), 3);

        let over = rd("sub1.example.com", &[]);
        info.add_domain(over.clone());
        assert_eq!(info.resolved_domains, [example, sub2, over.clone()]);
        assert_eq!(info.most_recent_domain(), Some(&over));
    }

    #[test]
    fn cname_chain() {
        let mut map = HashMap::new();
        map.insert("a.".to_string(), "b.".to_string());
        map.insert("b.".to_string(), "c.".to_string());
        map.insert("c.".to_string(), "a.".to_string());
        let mut r = ResolvedDomain {
            domain: "a.".into(),
            ..ResolvedDomain::default()
        };
        r.add_cnames(&map);
        assert_eq!(r.cnames, Some(vec!["b.".into(), "c.".into()]));
        assert_eq!(r.to_string(), "a. (-> b.->c.)");

        let mut none = ResolvedDomain {
            domain: "x.".into(),
            ..ResolvedDomain::default()
        };
        none.add_cnames(&map);
        assert_eq!(none.cnames, None);
    }
}
