//! Port of Go `netutils.IsValidFqdn` and the miekg/dns helpers it uses.

use std::sync::LazyLock;

use regex::Regex;

static CLEAN_DOMAIN: LazyLock<Regex> = LazyLock::new(|| {
    Regex::new(r"^((xn--)?[a-z0-9_-]{1,63}\.)*(xn--)?[a-z0-9_-]{1,63}\.$").expect("valid regex")
});

// Copied from Go, including its `\a-z` range (BEL up to 'z'), which makes the
// class much wider than it reads. Rust needs `[` escaped inside a class.
static DNS_SD_DOMAIN: LazyLock<Regex> = LazyLock::new(|| {
    Regex::new(concat!(
        r"^[",
        r" !#$%&()*+,\-\./0-9:;=?@A-Z\[\\\]^_\a-z{|}~",
        "ŠŒŽšœžŸ¡¿ÀÁÂÃÄÅÆÇÈÉÊËÌÍÎÏÐÑÒÓÔÕÖØÙÚÛÜÝÞßàáâãäåæçèéêëìíîïðñòóôõöøùúûüýþÿ",
        r"]*$",
    ))
    .expect("valid regex")
});

/// Returns whether the given string is a valid fully qualified domain name.
pub fn is_valid_fqdn(fqdn: &str) -> bool {
    if fqdn == "." {
        return true;
    }
    if fqdn.len() > 256 {
        return false;
    }
    if !is_fqdn(fqdn) {
        return false;
    }
    if fqdn.ends_with(".local.") {
        return DNS_SD_DOMAIN.is_match(fqdn);
    }
    if !CLEAN_DOMAIN.is_match(fqdn) {
        return false;
    }
    is_domain_name(fqdn)
}

/// miekg/dns `IsFqdn`: ends with an unescaped dot.
fn is_fqdn(s: &str) -> bool {
    let Some(s) = s.strip_suffix('.') else {
        return false;
    };
    if !s.ends_with('\\') {
        return true;
    }
    let backslashes = s.bytes().rev().take_while(|&b| b == b'\\').count();
    backslashes % 2 == 0
}

/// miekg/dns `IsDomainName` (label and wire length limits).
fn is_domain_name(s: &str) -> bool {
    const LENMSG: usize = 256;
    if s.is_empty() {
        return false;
    }
    let owned;
    let s = if is_fqdn(s) {
        s.as_bytes()
    } else {
        owned = format!("{s}.");
        owned.as_bytes()
    };

    let mut off = 0usize;
    let mut begin = 0usize;
    let mut was_dot = false;
    let mut escape = false;
    let mut i = 0usize;
    while i < s.len() {
        match s[i] {
            b'\\' => {
                escape = !escape;
                if off + 1 > LENMSG {
                    return false;
                }
                let rest = &s[i + 1..];
                if rest.len() >= 3 && rest[..3].iter().all(u8::is_ascii_digit) {
                    i += 3;
                    begin += 3;
                } else {
                    i += 1;
                    begin += 1;
                }
                was_dot = false;
            }
            b'.' => {
                escape = false;
                if i == 0 && s.len() > 1 {
                    return false;
                }
                if was_dot {
                    return false;
                }
                was_dot = true;
                let label_len = i.saturating_sub(begin);
                if label_len >= 64 {
                    return false;
                }
                off += 1 + label_len;
                if off > LENMSG {
                    return false;
                }
                begin = i + 1;
            }
            _ => {
                escape = false;
                was_dot = false;
            }
        }
        i += 1;
    }
    !escape
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn go_dns_validation_cases() {
        let valid = [
            ".",
            "at.",
            "orf.at.",
            "www.orf.at.",
            "a.b.c.d.e.f.g.h.i.j.k.l.m.n.o.p.q.r.s.t.u.v.x.y.z.example.org.",
            "a_a.com.",
            "a-a.com.",
            "xn--a.com.",
            "xn--asdfasdfasdfasdfasdfasdfasdfasdfasdfasdfasdfasdfasdfasdfasd.com.",
            "-.com.",
            "_.com.",
            "a_.com.",
            "a-.com.",
            "_a.com.",
            "-a.com.",
            "iuqerfsodp9ifjaposdfjhgosurijfaewrwergwea.com.",
            "my_printer.local.",
        ];
        for d in valid {
            assert!(is_valid_fqdn(d), "{d} should be valid");
        }
        let long = format!("{}as.com.", "asdf.".repeat(50));
        let invalid = [
            ".com.",
            "xn--asdfasdfasdfasdfasdfasdfasdfasdfasdfasdfasdfasdfasdfasdfasdf.com.",
            "asdfasdfasdfasdfasdfasdfasdfasdfasdfasdfasdfasdfasdfasdfasdfasdf.com.",
            long.as_str(),
            "example.com",
            "a..b.com.",
            "Example.com.",
            "",
        ];
        for d in invalid {
            assert!(!is_valid_fqdn(d), "{d} should be invalid");
        }
    }
}
