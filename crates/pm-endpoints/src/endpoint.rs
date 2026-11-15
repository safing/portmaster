//! Rule parsing and rendering, port of Go `endpoints/endpoint*.go`.

use std::fmt;
use std::net::IpAddr;
use std::sync::LazyLock;

use regex::Regex;

use crate::Error;

pub(crate) const SCOPE_LOCALHOST: u8 = 1;
pub(crate) const SCOPE_LAN: u8 = 2;
pub(crate) const SCOPE_INTERNET: u8 = 4;

static COUNTRY: LazyLock<Regex> = LazyLock::new(|| Regex::new(r"^[A-Z]{2}$").expect("regex"));
static CONTINENT: LazyLock<Regex> = LazyLock::new(|| Regex::new(r"^C:[A-Z]{2}$").expect("regex"));
static ASN: LazyLock<Regex> = LazyLock::new(|| Regex::new(r"^AS[0-9]+$").expect("regex"));
static ALLOWED_DOMAIN_CHARS: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r"^[a-z0-9\.-]+$").expect("regex"));
static LOOKS_LIKE_AN_IP: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r"^[0-9\.:]+$").expect("regex"));

/// How a domain rule compares domains.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
#[repr(u8)]
pub enum DomainMatchType {
    Exact = 0,
    Zone = 1,
    Suffix = 2,
    Prefix = 3,
    Contains = 4,
}

/// An IP network, like Go `net.IPNet` from `net.ParseCIDR`.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct IpNet {
    pub network: IpAddr,
    pub prefix: u8,
}

impl IpNet {
    /// Parses CIDR notation; the address is masked to the network.
    pub fn parse(s: &str) -> Option<IpNet> {
        let (addr, prefix) = s.split_once('/')?;
        if prefix.is_empty() || !prefix.bytes().all(|b| b.is_ascii_digit()) || prefix.len() > 3 {
            return None;
        }
        let prefix: u8 = prefix.parse().ok()?;
        let ip = parse_ip(addr)?;
        let max = if ip.is_ipv4() { 32 } else { 128 };
        if prefix > max {
            return None;
        }
        Some(IpNet {
            network: mask(ip, prefix),
            prefix,
        })
    }

    /// Returns whether the network contains the IP. IPv4 and IPv6 never mix.
    pub fn contains(&self, ip: IpAddr) -> bool {
        let ip = ip.to_canonical();
        ip.is_ipv4() == self.network.is_ipv4() && mask(ip, self.prefix) == self.network
    }
}

impl fmt::Display for IpNet {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "{}/{}", self.network, self.prefix)
    }
}

fn mask(ip: IpAddr, prefix: u8) -> IpAddr {
    match ip {
        IpAddr::V4(v4) => {
            let bits = u32::from(v4);
            let m = u32::MAX.checked_shl(32 - u32::from(prefix)).unwrap_or(0);
            IpAddr::V4((bits & m).into())
        }
        IpAddr::V6(v6) => {
            let bits = u128::from(v6);
            let m = u128::MAX.checked_shl(128 - u32::from(prefix)).unwrap_or(0);
            IpAddr::V6((bits & m).into())
        }
    }
}

/// Parses an IP like Go `net.ParseIP`; IPv4-mapped IPv6 becomes IPv4.
fn parse_ip(s: &str) -> Option<IpAddr> {
    s.parse::<IpAddr>().ok().map(|ip| ip.to_canonical())
}

/// What a rule matches on.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum EndpointKind {
    Any,
    Ip(IpAddr),
    IpRange(IpNet),
    Country(String),
    Continent(String),
    Asn(u64),
    Scope(u8),
    Lists {
        list_set: Vec<String>,
        lists: String,
    },
    Domain {
        original_value: String,
        domain: String,
        domain_zone: String,
        match_type: DomainMatchType,
    },
}

/// A parsed endpoint rule.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Endpoint {
    pub protocol: u8,
    pub start_port: u16,
    pub end_port: u16,
    pub permitted: bool,
    pub kind: EndpointKind,
}

/// Parses a single rule, like Go `parseEndpoint`.
pub fn parse_endpoint(value: &str) -> Result<Endpoint, Error> {
    let mut fields: Vec<&str> = value.split_whitespace().collect();
    if fields.len() < 2 {
        return Err(Error(format!(r#"invalid endpoint definition: "{value}""#)));
    }
    if let Some(i) = fields.iter().position(|f| f.starts_with('#')) {
        fields.truncate(i);
    }
    // Go indexes fields[1] here and panics on rules like "+ # comment".
    if fields.len() < 2 {
        return Err(Error(format!(r#"invalid endpoint definition: "{value}""#)));
    }

    let kind = parse_kind(&fields)?
        .ok_or_else(|| Error(format!(r#"unknown endpoint definition: "{value}""#)))?;
    parse_ppp(kind, &fields)
}

fn parse_kind(fields: &[&str]) -> Result<Option<EndpointKind>, Error> {
    let value = fields[1];

    if value == "*" {
        return Ok(Some(EndpointKind::Any));
    }
    if let Some(ip) = parse_ip(value) {
        return Ok(Some(EndpointKind::Ip(ip)));
    }
    if let Some(net) = IpNet::parse(value) {
        return Ok(Some(EndpointKind::IpRange(net)));
    }
    if COUNTRY.is_match(value) {
        return Ok(Some(EndpointKind::Country(value.to_uppercase())));
    }
    if CONTINENT.is_match(value) {
        let code = value.to_uppercase();
        return Ok(Some(EndpointKind::Continent(
            code.trim_start_matches("C:").to_string(),
        )));
    }
    if ASN.is_match(value) {
        let digits = &value[2..];
        let asn = digits
            .parse::<u64>()
            .map_err(|_| Error(format!("failed to parse AS number {digits}")))?;
        return Ok(Some(EndpointKind::Asn(asn)));
    }
    if let Some(scopes) = parse_scopes(value) {
        return Ok(Some(EndpointKind::Scope(scopes)));
    }
    if let Some(rest) = value.strip_prefix("L:") {
        let list_set: Vec<String> = rest.split(',').map(str::to_string).collect();
        let lists = format!("L:{}", list_set.join(","));
        return Ok(Some(EndpointKind::Lists { list_set, lists }));
    }
    parse_domain(value)
}

fn parse_scopes(value: &str) -> Option<u8> {
    let mut scopes = 0u8;
    for val in value.to_lowercase().split(',') {
        // Go toggles bits, so a repeated scope cancels itself out.
        match val {
            "localhost" => scopes ^= SCOPE_LOCALHOST,
            "lan" => scopes ^= SCOPE_LAN,
            "internet" => scopes ^= SCOPE_INTERNET,
            _ => return None,
        }
    }
    Some(scopes)
}

fn parse_domain(value: &str) -> Result<Option<EndpointKind>, Error> {
    let mut domain = value.to_string();
    if !domain.ends_with(['.', '*']) {
        domain.push('.');
    }
    if LOOKS_LIKE_AN_IP.is_match(&domain) {
        return Ok(None);
    }
    let domain = domain.to_lowercase();

    let mut need_valid_fqdn = true;
    let mut zone = String::new();
    let (match_type, matched) = if domain.starts_with('*') && domain.ends_with('*') {
        need_valid_fqdn = false;
        let inner = domain.strip_prefix('*').unwrap_or(&domain);
        let inner = inner.strip_suffix('*').unwrap_or(inner);
        (DomainMatchType::Contains, inner.to_string())
    } else if let Some(prefix) = domain.strip_suffix('*') {
        need_valid_fqdn = false;
        if prefix.starts_with('.') {
            return Ok(None);
        }
        if LOOKS_LIKE_AN_IP.is_match(prefix) {
            return Err(Error(
                "use CIDR notation (eg. 10.0.0.0/24) for matching ip address ranges".to_string(),
            ));
        }
        (DomainMatchType::Prefix, prefix.to_string())
    } else if let Some(suffix) = domain.strip_prefix('*') {
        need_valid_fqdn = false;
        (DomainMatchType::Suffix, suffix.to_string())
    } else if let Some(rest) = domain.strip_prefix('.') {
        zone = format!(".{rest}");
        (DomainMatchType::Zone, rest.to_string())
    } else {
        (DomainMatchType::Exact, domain.clone())
    };

    if need_valid_fqdn && !pm_netutils::is_valid_fqdn(&matched) {
        return Ok(None);
    }
    if !need_valid_fqdn && !ALLOWED_DOMAIN_CHARS.is_match(&matched) {
        return Ok(None);
    }
    if matched.contains("..") {
        return Ok(None);
    }

    Ok(Some(EndpointKind::Domain {
        original_value: value.to_string(),
        domain: matched,
        domain_zone: zone,
        match_type,
    }))
}

/// Go `strconv.ParseUint(s, 10, bits)`: digits only, no sign.
fn parse_uint<T: std::str::FromStr>(s: &str) -> Option<T> {
    if s.is_empty() || !s.bytes().all(|b| b.is_ascii_digit()) {
        return None;
    }
    s.parse().ok()
}

fn invalid(fields: &[&str], msg: &str) -> Error {
    Error(format!(
        r#"invalid endpoint definition: "{}" - {msg}"#,
        fields.join(" ")
    ))
}

fn parse_ppp(kind: EndpointKind, fields: &[&str]) -> Result<Endpoint, Error> {
    let mut ep = Endpoint {
        protocol: 0,
        start_port: 0,
        end_port: 0,
        permitted: false,
        kind,
    };

    match fields.len() {
        2 => {}
        3 => {
            let split: Vec<&str> = fields[2].split('/').collect();
            if split.len() > 2 {
                return Err(invalid(
                    fields,
                    "protocol and port must be in format <protocol>/<port>",
                ));
            }
            match split[0] {
                "" => return Err(invalid(fields, "protocol can't be empty")),
                "*" => {}
                proto => {
                    ep.protocol = parse_uint::<u8>(proto)
                        .or_else(|| pm_netutils::protocol_number(proto))
                        .ok_or_else(|| invalid(fields, "protocol number parsing error"))?;
                }
            }
            if let Some(ports) = split.get(1) {
                if ports.is_empty() || *ports == "*" {
                    return Err(invalid(fields, "omit port if should match any"));
                }
                let port_split: Vec<&str> = ports.split('-').collect();
                if port_split.len() > 2 {
                    return Err(invalid(fields, "ports must be in format from-to"));
                }
                let parse_port = |s: &str| {
                    parse_uint::<u16>(s)
                        .or_else(|| pm_netutils::port_number(s))
                        .ok_or_else(|| invalid(fields, "port number parsing error"))
                };
                let mut port = parse_port(port_split[0])?;
                if port == 0 {
                    return Err(invalid(fields, "port number cannot be 0"));
                }
                ep.start_port = port;
                if let Some(end) = port_split.get(1) {
                    port = parse_port(end)?;
                }
                if port == 0 {
                    return Err(invalid(fields, "port number cannot be 0"));
                }
                ep.end_port = port;
            }
            if ep.protocol == 0 && ep.start_port == 0 {
                return Err(invalid(fields, "omit protocol/port if should match any"));
            }
        }
        _ => return Err(invalid(fields, "there should be only 2 or 3 segments")),
    }

    ep.permitted = match fields[0] {
        "+" => true,
        "-" => false,
        _ => return Err(invalid(fields, "invalid permission prefix")),
    };
    Ok(ep)
}

impl Endpoint {
    /// Returns the matched value as written in rules (without verdict and PPP).
    pub(crate) fn value_string(&self) -> String {
        match &self.kind {
            EndpointKind::Any => "*".to_string(),
            EndpointKind::Ip(ip) => ip.to_string(),
            EndpointKind::IpRange(net) => net.to_string(),
            EndpointKind::Country(code) => code.clone(),
            EndpointKind::Continent(code) => format!("C:{code}"),
            EndpointKind::Asn(asn) => format!("AS{asn}"),
            EndpointKind::Scope(scopes) => scope_names(*scopes),
            EndpointKind::Lists { lists, .. } => lists.clone(),
            EndpointKind::Domain { original_value, .. } => original_value.clone(),
        }
    }
}

pub(crate) fn scope_names(scopes: u8) -> String {
    match scopes {
        SCOPE_LOCALHOST => return "Localhost".to_string(),
        SCOPE_LAN => return "LAN".to_string(),
        SCOPE_INTERNET => return "Internet".to_string(),
        _ => {}
    }
    let mut names = Vec::new();
    if scopes & SCOPE_LOCALHOST > 0 {
        names.push("Localhost");
    }
    if scopes & SCOPE_LAN > 0 {
        names.push("LAN");
    }
    if scopes & SCOPE_INTERNET > 0 {
        names.push("Internet");
    }
    names.join(",")
}

impl fmt::Display for Endpoint {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        let verdict = if self.permitted { "+ " } else { "- " };
        write!(f, "{verdict}{}", self.value_string())?;
        if self.protocol > 0 || self.start_port > 0 {
            if self.protocol > 0 {
                write!(f, " {}", pm_netutils::protocol_name(self.protocol))?;
            } else {
                f.write_str(" *")?;
            }
            if self.start_port > 0 {
                if self.start_port == self.end_port {
                    write!(f, "/{}", pm_netutils::port_name(self.start_port))?;
                } else {
                    write!(f, "/{}-{}", self.start_port, self.end_port)?;
                }
            }
        }
        Ok(())
    }
}
