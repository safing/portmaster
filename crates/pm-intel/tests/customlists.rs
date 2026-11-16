//! Tests for the custom filter list parse and lookups (Go
//! `service/intel/customlists`).

use std::io::Write;
use std::path::PathBuf;
use std::sync::atomic::{AtomicU32, Ordering};

use pm_intel::customlists::{split_domain, CustomLists};

static COUNTER: AtomicU32 = AtomicU32::new(0);

struct TempFile(PathBuf);

impl TempFile {
    fn with(content: &str) -> TempFile {
        let name = format!(
            "pm-intel-customlists-{}-{}",
            std::process::id(),
            COUNTER.fetch_add(1, Ordering::Relaxed)
        );
        let path = std::env::temp_dir().join(name);
        let mut file = std::fs::File::create(&path).expect("create temp file");
        file.write_all(content.as_bytes()).expect("write list");
        TempFile(path)
    }

    fn path_str(&self) -> &str {
        self.0.to_str().expect("utf-8 path")
    }
}

impl Drop for TempFile {
    fn drop(&mut self) {
        let _ = std::fs::remove_file(&self.0);
    }
}

const LIST: &str = "\
# A comment line
example.com
sub.example.com extra fields are ignored
1.2.3.4
::ffff:5.6.7.8
US
DE
AS1234
0.0.0.0
::
AS99999999999
not a domain !!
Example.com
https://example.org
";

#[test]
fn parses_entries_and_counts_invalid_lines() {
    let file = TempFile::with(LIST);
    let lists = CustomLists::new();
    let report = lists
        .parse_file(file.path_str())
        .expect("parse list")
        .expect("report");

    // Domains: example.com, sub.example.com and "not." - Go only looks at the
    // first field, and "not" alone is a valid FQDN.
    assert_eq!(report.domains, 3);
    // IPs: 1.2.3.4 and the canonical form of ::ffff:5.6.7.8 (that is 5.6.7.8;
    // it is a distinct entry from 1.2.3.4, as in Go).
    assert_eq!(report.ips, 2);
    assert_eq!(report.asns, 1);
    assert_eq!(report.countries, 2);
    assert_eq!(report.entries(), 8);

    assert_eq!(report.total_lines, 14);
    // Zero IPs (2), the out-of-range AS, the upper-case domain and the URL are
    // invalid; comments and duplicates are valid.
    assert_eq!(report.invalid_lines, 5);
    assert!(report.too_many_invalid_lines());

    assert!(lists.is_loaded());
}

#[test]
fn lookups_match_go() {
    let file = TempFile::with("example.com\nsub.example.com\n1.2.3.4\nUS\nAS1234\n");
    let lists = CustomLists::new();
    lists.parse_file(file.path_str()).expect("parse");

    // Exact lookups need the trailing dot, like Go's `dns.Fqdn` entries.
    assert_eq!(
        lists.lookup_domain("sub.example.com.", false),
        (true, "sub.example.com.".to_string())
    );
    assert_eq!(
        lists.lookup_domain("sub.example.com", false),
        (false, "sub.example.com".to_string())
    );

    // Sub-domain lookups walk up to the public suffix.
    assert_eq!(
        lists.lookup_domain("deep.sub.example.com.", true),
        (true, "sub.example.com.".to_string())
    );
    assert_eq!(
        lists.lookup_domain("deep.other.com.", true),
        (false, String::new())
    );

    assert!(lists.lookup_ip("1.2.3.4".parse().unwrap()));
    // IPv4-mapped addresses are canonicalized to IPv4, like Go's `ip.String()`.
    assert!(lists.lookup_ip("::ffff:1.2.3.4".parse().unwrap()));
    assert!(!lists.lookup_ip("1.2.3.5".parse().unwrap()));

    assert!(lists.lookup_asn(1234));
    assert!(!lists.lookup_asn(1235));

    assert!(lists.lookup_country("US"));
    assert!(!lists.lookup_country("us"));
}

#[test]
fn empty_path_resets_the_list() {
    let file = TempFile::with("example.com\n");
    let lists = CustomLists::new();
    lists.parse_file(file.path_str()).expect("parse");
    assert!(lists.is_loaded());

    assert_eq!(lists.parse_file("").expect("empty path"), None);
    assert!(!lists.is_loaded());
}

#[test]
fn missing_file_is_an_error_and_keeps_previous_entries() {
    let file = TempFile::with("example.com\n");
    let lists = CustomLists::new();
    lists.parse_file(file.path_str()).expect("parse");
    assert!(lists.is_loaded());

    // Go clears the maps before parsing, but keeps what it parsed so far on an
    // error - and nothing was parsed, so the list ends up empty.
    assert!(lists.parse_file("/nonexistent/custom-list.txt").is_err());
    assert!(!lists.is_loaded());
}

#[test]
fn split_domain_matches_go() {
    // Reference output of Go `service/intel/customlists.splitDomain`.
    let cases: &[(&str, &[&str])] = &[
        ("example.com.", &["example.com."]),
        (
            "deep.sub.example.com.",
            &["deep.sub.example.com.", "sub.example.com.", "example.com."],
        ),
        ("appspot.com.", &["appspot.com"]),
        ("www.appspot.com.", &["www.appspot.com."]),
        (
            "foo.bar.appspot.com.",
            &["foo.bar.appspot.com.", "bar.appspot.com."],
        ),
        ("foo.unknown", &["foo.unknown."]),
        ("unknowntld.", &["unknowntld"]),
        (
            "a.b.unknowntld.",
            &["a.b.unknowntld.", "b.unknowntld."],
        ),
        ("com.", &["com"]),
        (".", &[""]),
        ("co.uk.", &["co.uk"]),
        ("a.b.co.uk.", &["a.b.co.uk.", "b.co.uk."]),
        ("foo.ck.", &["foo.ck"]),
        ("www.ck.", &["www.ck."]),
        ("a.www.ck.", &["a.www.ck.", "www.ck."]),
        ("xn--80ak6aa92e.com.", &["xn--80ak6aa92e.com."]),
        ("localhost.", &["localhost"]),
        ("1.2.3.4.", &["1.2.3.4"]),
    ];

    for (domain, expected) in cases {
        let expected: Vec<String> = expected.iter().map(|entry| entry.to_string()).collect();
        assert_eq!(split_domain(domain), expected, "splitDomain({domain:?})");
    }
}
