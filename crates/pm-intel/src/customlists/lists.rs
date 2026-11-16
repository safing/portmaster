//! Parsing and lookup of the custom filter list, port of Go
//! `service/intel/customlists/lists.go` and the lookup functions of
//! `module.go`.

use std::collections::BTreeSet;
use std::fs::File;
use std::io::{self, BufRead, BufReader};
use std::net::IpAddr;
use std::path::Path;
use std::sync::RwLock;

use pm_netutils::is_valid_fqdn;

/// Ratio of invalid lines that is tolerated before a warning is emitted.
pub const INVALID_LINES_WARNING_RATIO: f32 = 0.1;

/// Go's `bufio.Scanner` gives up on lines longer than 64 KiB.
const MAX_LINE_SIZE: usize = 64 * 1024;

/// The parsed entries of the custom filter list.
#[derive(Debug, Default, Clone, PartialEq, Eq)]
struct ListsData {
    countries: BTreeSet<String>,
    ips: BTreeSet<IpAddr>,
    asns: BTreeSet<u32>,
    domains: BTreeSet<String>,
}

impl ListsData {
    fn is_loaded(&self) -> bool {
        !(self.domains.is_empty()
            && self.ips.is_empty()
            && self.countries.is_empty()
            && self.asns.is_empty())
    }
}

/// Statistics of one parse run, used for logging, states and notifications.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ParseReport {
    /// Path the list was read from.
    pub path: String,
    /// Number of loaded domains.
    pub domains: usize,
    /// Number of loaded IP addresses.
    pub ips: usize,
    /// Number of loaded autonomous systems.
    pub asns: usize,
    /// Number of loaded country codes.
    pub countries: usize,
    /// Number of lines in the file.
    pub total_lines: u64,
    /// Lines that are neither entries nor comments/empty lines.
    pub invalid_lines: u64,
}

impl ParseReport {
    /// Total number of loaded entries.
    pub fn entries(&self) -> usize {
        self.domains + self.ips + self.asns + self.countries
    }

    /// Returns whether too many lines could not be parsed, like Go's
    /// `invalidLinesRation > rationForInvalidLinesUntilWarning`.
    pub fn too_many_invalid_lines(&self) -> bool {
        let ratio = self.invalid_lines as f32 / self.total_lines as f32;
        ratio > INVALID_LINES_WARNING_RATIO
    }
}

/// A custom filter list.
///
/// Go keeps the four maps in package globals guarded by an `RWMutex`; this type
/// owns them behind an `RwLock`, so the module state stays explicit.
#[derive(Debug, Default)]
pub struct CustomLists {
    data: RwLock<ListsData>,
}

impl CustomLists {
    /// Creates an empty custom filter list.
    pub fn new() -> CustomLists {
        CustomLists::default()
    }

    /// Returns whether any entry is loaded.
    pub fn is_loaded(&self) -> bool {
        self.read().is_loaded()
    }

    /// Drops all entries.
    pub fn reset(&self) {
        let mut data = self.write();
        *data = ListsData::default();
    }

    /// Reads the list from `path`, replacing the current entries.
    ///
    /// Returns `Ok(None)` when no file path is configured, like Go's early
    /// return for an empty path. The entries are cleared before the file is
    /// opened, matching Go.
    pub fn parse_file(&self, path: &str) -> io::Result<Option<ParseReport>> {
        let mut data = ListsData::default();

        if path.is_empty() {
            *self.write() = data;
            return Ok(None);
        }

        let report = match parse_into(Path::new(path), &mut data) {
            Ok(report) => report,
            Err(error) => {
                // Go mutates the package-level maps while parsing, so entries
                // read before the error are kept.
                *self.write() = data;
                return Err(error);
            }
        };

        *self.write() = data;
        Ok(Some(report))
    }

    /// Checks whether the domain (or one of its parent domains) is listed.
    ///
    /// Returns whether it matched and the matching entry.
    pub fn lookup_domain(&self, full_domain: &str, filter_subdomains: bool) -> (bool, String) {
        let data = self.read();

        if filter_subdomains {
            for domain in split_domain(full_domain) {
                if data.domains.contains(&domain) {
                    return (true, domain);
                }
            }
            return (false, String::new());
        }

        (
            data.domains.contains(full_domain),
            full_domain.to_string(),
        )
    }

    /// Checks whether the IP address is listed.
    pub fn lookup_ip(&self, ip: IpAddr) -> bool {
        self.read().ips.contains(&ip.to_canonical())
    }

    /// Checks whether the autonomous system number is listed.
    pub fn lookup_asn(&self, number: u32) -> bool {
        self.read().asns.contains(&number)
    }

    /// Checks whether the country code is listed.
    pub fn lookup_country(&self, country_code: &str) -> bool {
        self.read().countries.contains(country_code)
    }

    fn read(&self) -> std::sync::RwLockReadGuard<'_, ListsData> {
        self.data.read().unwrap_or_else(|error| error.into_inner())
    }

    fn write(&self) -> std::sync::RwLockWriteGuard<'_, ListsData> {
        self.data.write().unwrap_or_else(|error| error.into_inner())
    }
}

/// Parses the file into `data`.
fn parse_into(path: &Path, data: &mut ListsData) -> io::Result<ParseReport> {
    let file = File::open(path)?;
    let mut reader = BufReader::new(file);

    let mut total_lines: u64 = 0;
    let mut invalid_lines: u64 = 0;
    let mut line = Vec::new();

    loop {
        line.clear();
        let read = reader.read_until(b'\n', &mut line)?;
        if read == 0 {
            break;
        }

        if line.len() > MAX_LINE_SIZE {
            return Err(io::Error::new(
                io::ErrorKind::InvalidData,
                "bufio.Scanner: token too long",
            ));
        }

        // `bufio.ScanLines` drops the newline and a preceding carriage return.
        if line.last() == Some(&b'\n') {
            line.pop();
            if line.last() == Some(&b'\r') {
                line.pop();
            }
        }

        total_lines += 1;
        let text = String::from_utf8_lossy(&line);
        if !parse_line(&text, data) {
            invalid_lines += 1;
        }
    }

    Ok(ParseReport {
        path: path.display().to_string(),
        domains: data.domains.len(),
        ips: data.ips.len(),
        asns: data.asns.len(),
        countries: data.countries.len(),
        total_lines,
        invalid_lines,
    })
}

/// Returns whether the given line was valid; invalid entries are not added.
///
/// Comments, empty lines and duplicates count as valid lines, like in Go.
fn parse_line(line: &str, data: &mut ListsData) -> bool {
    // Everything after the first field is ignored.
    let Some(field) = line.split_whitespace().next() else {
        // Empty line: not an entry, but a valid line.
        return true;
    };

    // Ignore comments.
    if field.starts_with('#') {
        return true;
    }

    // Check if it is a country code (checked before AS and IP, like Go).
    if is_country_code(field) {
        data.countries.insert(field.to_string());
        return true;
    }

    // Check if it is an autonomous system (example: AS123).
    if is_autonomous_system(field) {
        let Ok(number) = field[2..].parse::<u32>() else {
            return false;
        };
        data.asns.insert(number);
        return true;
    }

    // Try to parse an IP address.
    if let Ok(ip) = field.parse::<IpAddr>() {
        // Zero IPs are almost always a sign of a broken list.
        if ip.is_unspecified() {
            return false;
        }
        data.ips.insert(ip.to_canonical());
        return true;
    }

    // Check if it is a domain.
    let domain = fqdn(field);
    if is_valid_fqdn(&domain) {
        data.domains.insert(domain);
        return true;
    }

    false
}

/// miekg/dns `Fqdn`: appends a dot unless the name already ends in an
/// unescaped one.
fn fqdn(s: &str) -> String {
    if is_fqdn(s) {
        s.to_string()
    } else {
        format!("{s}.")
    }
}

/// miekg/dns `IsFqdn`.
fn is_fqdn(s: &str) -> bool {
    let Some(stripped) = s.strip_suffix('.') else {
        return false;
    };
    if !stripped.ends_with('\\') {
        return true;
    }
    let backslashes = stripped.bytes().rev().take_while(|&byte| byte == b'\\').count();
    backslashes % 2 == 0
}

/// Go `isCountryCode`: `^[A-Z]{2}$`.
fn is_country_code(field: &str) -> bool {
    let bytes = field.as_bytes();
    bytes.len() == 2 && bytes.iter().all(|byte| byte.is_ascii_uppercase())
}

/// Go `isAutonomousSystem`: `^AS[0-9]+$`.
fn is_autonomous_system(field: &str) -> bool {
    field
        .strip_prefix("AS")
        .is_some_and(|number| !number.is_empty() && number.bytes().all(|byte| byte.is_ascii_digit()))
}

/// Splits the domain into itself and its parent domains, stopping at the
/// public suffix, like Go `splitDomain` in this package.
pub fn split_domain(domain: &str) -> Vec<String> {
    let domain = domain.trim_matches('.');
    // Go's `publicsuffix.PublicSuffix` returns IP addresses unchanged.
    if domain.parse::<IpAddr>().is_ok() {
        return vec![domain.to_string()];
    }
    let Some(suffix) = public_suffix(domain) else {
        return vec![domain.to_string()];
    };
    if suffix == domain {
        return vec![domain.to_string()];
    }

    let without_suffix = domain[..domain.len() - suffix.len()].trim_matches('.');
    let labels: Vec<&str> = without_suffix
        .split('.')
        .filter(|label| !label.is_empty())
        .collect();

    let mut domains = Vec::with_capacity(labels.len());
    for index in 0..labels.len() {
        let mut entry = format!("{}.{}", labels[index..].join("."), suffix);
        if !entry.ends_with('.') {
            entry.push('.');
        }
        domains.push(entry);
    }
    domains
}

/// Go `publicsuffix.PublicSuffix`: returns the public suffix itself when the
/// domain is unknown to the PSL.
fn public_suffix(domain: &str) -> Option<&str> {
    if domain.is_empty() {
        return None;
    }

    let typed = psl::suffix(domain.as_bytes())?;
    let suffix = std::str::from_utf8(typed.as_bytes()).ok()?;
    Some(suffix)
}
