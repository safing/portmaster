//! iptables rules, port of Go `interception/nfqueue_linux.go`.
//!
//! Rules are copied verbatim from Go so that both cores install, find and
//! remove the same chains.

use std::process::Command;

/// Queue numbers used by the rules.
pub const QUEUE_V4_OUT: u16 = 17040;
pub const QUEUE_V4_IN: u16 = 17140;
pub const QUEUE_V6_OUT: u16 = 17060;
pub const QUEUE_V6_IN: u16 = 17160;

pub const V4_CHAINS: &[&str] = &[
    "mangle PORTMASTER-INGEST-OUTPUT",
    "mangle PORTMASTER-INGEST-INPUT",
    "filter PORTMASTER-FILTER",
    "nat PORTMASTER-REDIRECT",
];

pub const V4_RULES: &[&str] = &[
    "mangle PORTMASTER-INGEST-OUTPUT -m mark ! --mark 0 -m connmark --mark 1710 -j RETURN",
    "mangle PORTMASTER-INGEST-OUTPUT -m mark ! --mark 0 -m connmark --mark 1709 -j RETURN",
    "mangle PORTMASTER-INGEST-OUTPUT -j CONNMARK --restore-mark",
    "mangle PORTMASTER-INGEST-OUTPUT -m mark --mark 0 -j NFQUEUE --queue-num 17040 --queue-bypass",
    "mangle PORTMASTER-INGEST-INPUT -m mark ! --mark 0 -m connmark --mark 1710 -j RETURN",
    "mangle PORTMASTER-INGEST-INPUT -m mark ! --mark 0 -m connmark --mark 1709 -j RETURN",
    "mangle PORTMASTER-INGEST-INPUT -j CONNMARK --restore-mark",
    "mangle PORTMASTER-INGEST-INPUT -m mark --mark 0 -j NFQUEUE --queue-num 17140 --queue-bypass",
    "filter PORTMASTER-FILTER -m mark --mark 0 -j DROP",
    "filter PORTMASTER-FILTER -m connmark --mark 1710 -j RETURN",
    "filter PORTMASTER-FILTER -m connmark --mark 1709 -j ACCEPT",
    "filter PORTMASTER-FILTER -m mark --mark 1700 -j RETURN",
    "filter PORTMASTER-FILTER -m mark --mark 1701 -p icmp -j RETURN",
    "filter PORTMASTER-FILTER -m mark --mark 1701 -j REJECT --reject-with icmp-admin-prohibited",
    "filter PORTMASTER-FILTER -m mark --mark 1702 -j DROP",
    "filter PORTMASTER-FILTER -j CONNMARK --save-mark",
    "filter PORTMASTER-FILTER -m mark --mark 1710 -j RETURN",
    "filter PORTMASTER-FILTER -m mark --mark 1709 -j ACCEPT",
    "filter PORTMASTER-FILTER -m mark --mark 1711 -p icmp -j RETURN",
    "filter PORTMASTER-FILTER -m mark --mark 1711 -j REJECT --reject-with icmp-admin-prohibited",
    "filter PORTMASTER-FILTER -m mark --mark 1712 -j DROP",
    "filter PORTMASTER-FILTER -m mark --mark 1717 -j RETURN",
    "filter PORTMASTER-FILTER -m mark --mark 1719 -j RETURN",
    "nat PORTMASTER-REDIRECT -m mark --mark 1799 -p udp -j DNAT --to 127.0.0.17:53",
    "nat PORTMASTER-REDIRECT -m mark --mark 1717 -p tcp -j DNAT --to 127.0.0.17:717",
    "nat PORTMASTER-REDIRECT -m mark --mark 1717 -p udp -j DNAT --to 127.0.0.17:717",
    "nat PORTMASTER-REDIRECT -m mark --mark 1719 -p tcp -j DNAT --to 127.0.0.17:719",
    "nat PORTMASTER-REDIRECT -m mark --mark 1719 -p udp -j DNAT --to 127.0.0.17:719",
];

pub const V6_CHAINS: &[&str] = V4_CHAINS;

pub const V6_RULES: &[&str] = &[
    "mangle PORTMASTER-INGEST-OUTPUT -m mark ! --mark 0 -m connmark --mark 1710 -j RETURN",
    "mangle PORTMASTER-INGEST-OUTPUT -m mark ! --mark 0 -m connmark --mark 1709 -j RETURN",
    "mangle PORTMASTER-INGEST-OUTPUT -j CONNMARK --restore-mark",
    "mangle PORTMASTER-INGEST-OUTPUT -m mark --mark 0 -j NFQUEUE --queue-num 17060 --queue-bypass",
    "mangle PORTMASTER-INGEST-INPUT -m mark ! --mark 0 -m connmark --mark 1710 -j RETURN",
    "mangle PORTMASTER-INGEST-INPUT -m mark ! --mark 0 -m connmark --mark 1709 -j RETURN",
    "mangle PORTMASTER-INGEST-INPUT -j CONNMARK --restore-mark",
    "mangle PORTMASTER-INGEST-INPUT -m mark --mark 0 -j NFQUEUE --queue-num 17160 --queue-bypass",
    "filter PORTMASTER-FILTER -m mark --mark 0 -j DROP",
    "filter PORTMASTER-FILTER -m connmark --mark 1710 -j RETURN",
    "filter PORTMASTER-FILTER -m connmark --mark 1709 -j ACCEPT",
    "filter PORTMASTER-FILTER -m mark --mark 1700 -j RETURN",
    "filter PORTMASTER-FILTER -m mark --mark 1701 -p icmpv6 -j RETURN",
    "filter PORTMASTER-FILTER -m mark --mark 1701 -j REJECT --reject-with icmp6-adm-prohibited",
    "filter PORTMASTER-FILTER -m mark --mark 1702 -j DROP",
    "filter PORTMASTER-FILTER -j CONNMARK --save-mark",
    "filter PORTMASTER-FILTER -m mark --mark 1710 -j RETURN",
    "filter PORTMASTER-FILTER -m mark --mark 1709 -j ACCEPT",
    "filter PORTMASTER-FILTER -m mark --mark 1711 -p icmpv6 -j RETURN",
    "filter PORTMASTER-FILTER -m mark --mark 1711 -j REJECT --reject-with icmp6-adm-prohibited",
    "filter PORTMASTER-FILTER -m mark --mark 1712 -j DROP",
    "filter PORTMASTER-FILTER -m mark --mark 1717 -j RETURN",
    "filter PORTMASTER-FILTER -m mark --mark 1719 -j RETURN",
    "nat PORTMASTER-REDIRECT -m mark --mark 1799 -p udp -j DNAT --to [::1]:53",
    "nat PORTMASTER-REDIRECT -m mark --mark 1717 -p tcp -j DNAT --to [::1]:717",
    "nat PORTMASTER-REDIRECT -m mark --mark 1717 -p udp -j DNAT --to [::1]:717",
    "nat PORTMASTER-REDIRECT -m mark --mark 1719 -p tcp -j DNAT --to [::1]:719",
    "nat PORTMASTER-REDIRECT -m mark --mark 1719 -p udp -j DNAT --to [::1]:719",
];

/// Jump rules into the Portmaster chains; kept at position 1.
pub const ONCE: &[&str] = &[
    "mangle OUTPUT -j PORTMASTER-INGEST-OUTPUT",
    "mangle INPUT -j PORTMASTER-INGEST-INPUT",
    "filter OUTPUT -j PORTMASTER-FILTER",
    "filter INPUT -j PORTMASTER-FILTER",
    "nat OUTPUT -j PORTMASTER-REDIRECT",
];

const TEMPORARY_RULE: &[&str] = &["-m", "comment", "--comment", "TEMPORARY_RULE"];

/// An iptables failure.
#[derive(Debug, thiserror::Error)]
pub enum Error {
    #[error("running {binary}: {source}")]
    Spawn {
        binary: String,
        source: std::io::Error,
    },
    #[error("{binary} {args}: exit {code:?}: {stderr}")]
    Failed {
        binary: String,
        args: String,
        code: Option<i32>,
        stderr: String,
    },
    #[error("{0}")]
    Multiple(String),
}

/// Runs iptables commands; abstracted for tests.
pub trait Runner: Send + Sync {
    /// Runs `<binary> -w <args>` and returns stdout.
    fn run(&self, args: &[&str]) -> Result<String, Error>;
}

/// Runs the real `iptables`/`ip6tables` binary, like go-iptables.
pub struct Cmd {
    binary: String,
}

impl Cmd {
    pub fn v4() -> Cmd {
        Cmd {
            binary: "iptables".to_string(),
        }
    }

    pub fn v6() -> Cmd {
        Cmd {
            binary: "ip6tables".to_string(),
        }
    }
}

impl Runner for Cmd {
    fn run(&self, args: &[&str]) -> Result<String, Error> {
        let output = Command::new(&self.binary)
            .arg("-w")
            .args(args)
            .output()
            .map_err(|source| Error::Spawn {
                binary: self.binary.clone(),
                source,
            })?;
        if !output.status.success() {
            return Err(Error::Failed {
                binary: self.binary.clone(),
                args: args.join(" "),
                code: output.status.code(),
                stderr: String::from_utf8_lossy(&output.stderr).trim().to_string(),
            });
        }
        Ok(String::from_utf8_lossy(&output.stdout).into_owned())
    }
}

/// One rule string split into table, chain and spec, like Go `strings.Split(rule, " ")`.
struct Rule<'a> {
    table: &'a str,
    chain: &'a str,
    spec: Vec<&'a str>,
}

fn split(rule: &str) -> Rule<'_> {
    let mut parts = rule.split(' ');
    let table = parts.next().unwrap_or_default();
    let chain = parts.next().unwrap_or_default();
    Rule {
        table,
        chain,
        spec: parts.collect(),
    }
}

fn args<'a>(table: &'a str, op: &'a str, chain: &'a str, extra: &[&'a str]) -> Vec<&'a str> {
    let mut a = vec!["-t", table, op, chain];
    a.extend_from_slice(extra);
    a
}

fn chain_exists(r: &dyn Runner, table: &str, chain: &str) -> Result<bool, Error> {
    let out = r.run(&["-t", table, "-S"])?;
    let wanted = format!("-N {chain}");
    let policy = format!("-P {chain} ");
    Ok(out.lines().any(|l| l == wanted || l.starts_with(&policy)))
}

/// go-iptables `ClearChain`: create the chain or flush it.
fn clear_chain(r: &dyn Runner, table: &str, chain: &str) -> Result<(), Error> {
    let op = if chain_exists(r, table, chain)? {
        "-F"
    } else {
        "-N"
    };
    r.run(&["-t", table, op, chain]).map(drop)
}

fn exists(r: &dyn Runner, rule: &Rule<'_>) -> Result<bool, Error> {
    match r.run(&args(rule.table, "-C", rule.chain, &rule.spec)) {
        Ok(_) => Ok(true),
        // iptables -C exits 1 when the rule does not exist.
        Err(Error::Failed { code: Some(1), .. }) => Ok(false),
        Err(e) => Err(e),
    }
}

/// Installs the chains, rules and jump rules (Go `activateIPTables`).
pub fn activate(
    r: &dyn Runner,
    rules: &[&str],
    once: &[&str],
    chains: &[&str],
) -> Result<(), Error> {
    for chain in chains {
        let c = split(chain);
        clear_chain(r, c.table, c.chain)?;
    }
    for rule in rules {
        let rule = split(rule);
        r.run(&args(rule.table, "-A", rule.chain, &rule.spec))?;
    }
    for rule in once {
        let rule = split(rule);
        if !exists(r, &rule)? {
            let mut extra = vec!["1"];
            extra.extend_from_slice(&rule.spec);
            r.run(&args(rule.table, "-I", rule.chain, &extra))?;
        }
    }
    Ok(())
}

/// Removes jump rules and Portmaster chains, collecting errors (Go `deactivateIPTables`).
pub fn deactivate(r: &dyn Runner, once: &[&str], chains: &[&str]) -> Result<(), Error> {
    let mut errors = Vec::new();
    for rule in once {
        let rule = split(rule);
        match exists(r, &rule) {
            Ok(true) => {
                if let Err(e) = r.run(&args(rule.table, "-D", rule.chain, &rule.spec)) {
                    errors.push(e.to_string());
                }
            }
            Ok(false) => {}
            Err(e) => errors.push(e.to_string()),
        }
    }
    for chain in chains {
        let c = split(chain);
        for op in ["-F", "-X"] {
            if let Err(e) = r.run(&["-t", c.table, op, c.chain]) {
                errors.push(e.to_string());
            }
        }
    }
    if errors.is_empty() {
        Ok(())
    } else {
        Err(Error::Multiple(errors.join("; ")))
    }
}

/// Moves displaced jump rules back to position 1 without a bypass window
/// (Go `reinsertDisplacedRules`). Returns the rules that were moved.
pub fn reinsert_displaced(r: &dyn Runner, once: &[&str]) -> Result<Vec<String>, Error> {
    let mut to_update = Vec::new();
    for once_rule in once {
        let rule = split(once_rule);
        let pm_chain = rule.spec.last().copied().unwrap_or_default();
        let listed = r.run(&["-t", rule.table, "-S", rule.chain])?;
        // Line 0 is the chain policy, line 1 the first rule.
        let first = listed.lines().nth(1).unwrap_or_default();
        if !first.ends_with(pm_chain) {
            to_update.push(*once_rule);
        }
    }

    let mut moved = Vec::new();
    for once_rule in to_update {
        let rule = split(once_rule);
        let mut tmp: Vec<&str> = TEMPORARY_RULE.to_vec();
        tmp.extend_from_slice(&rule.spec);

        let mut insert_tmp = vec!["1"];
        insert_tmp.extend_from_slice(&tmp);
        r.run(&args(rule.table, "-I", rule.chain, &insert_tmp))?;

        if exists(r, &rule)? {
            r.run(&args(rule.table, "-D", rule.chain, &rule.spec))?;
        }
        let mut insert = vec!["1"];
        insert.extend_from_slice(&rule.spec);
        r.run(&args(rule.table, "-I", rule.chain, &insert))?;
        r.run(&args(rule.table, "-D", rule.chain, &tmp))?;
        moved.push(once_rule.to_string());
    }
    Ok(moved)
}

#[cfg(test)]
mod tests {
    use std::sync::Mutex;

    use super::*;

    /// Records commands and answers `-S` and `-C` queries from a script.
    struct Fake {
        calls: Mutex<Vec<String>>,
        listing: String,
        rules_exist: bool,
    }

    impl Runner for Fake {
        fn run(&self, args: &[&str]) -> Result<String, Error> {
            let line = args.join(" ");
            self.calls.lock().unwrap().push(line.clone());
            if args.get(2) == Some(&"-C") && !self.rules_exist {
                return Err(Error::Failed {
                    binary: "fake".into(),
                    args: line,
                    code: Some(1),
                    stderr: String::new(),
                });
            }
            if args.get(2) == Some(&"-S") {
                return Ok(self.listing.clone());
            }
            Ok(String::new())
        }
    }

    fn fake(listing: &str, rules_exist: bool) -> Fake {
        Fake {
            calls: Mutex::new(Vec::new()),
            listing: listing.to_string(),
            rules_exist,
        }
    }

    #[test]
    fn activate_creates_chains_appends_rules_and_inserts_jumps() {
        let f = fake("-P OUTPUT ACCEPT\n", false);
        activate(&f, V4_RULES, ONCE, V4_CHAINS).unwrap();
        let calls = f.calls.lock().unwrap();
        assert!(calls.contains(&"-t mangle -N PORTMASTER-INGEST-OUTPUT".to_string()));
        assert!(calls.contains(
            &"-t mangle -A PORTMASTER-INGEST-OUTPUT -m mark --mark 0 -j NFQUEUE --queue-num 17040 --queue-bypass"
                .to_string()
        ));
        assert!(calls.contains(&"-t filter -I OUTPUT 1 -j PORTMASTER-FILTER".to_string()));
        assert_eq!(
            calls.iter().filter(|c| c.contains(" -A ")).count(),
            V4_RULES.len()
        );
    }

    #[test]
    fn existing_chain_is_flushed_and_existing_jump_is_kept() {
        let f = fake("-N PORTMASTER-FILTER\n-N PORTMASTER-INGEST-OUTPUT\n", true);
        activate(&f, &[], ONCE, &["filter PORTMASTER-FILTER"]).unwrap();
        let calls = f.calls.lock().unwrap();
        assert!(calls.contains(&"-t filter -F PORTMASTER-FILTER".to_string()));
        assert!(!calls.iter().any(|c| c.contains(" -I ")));
    }

    #[test]
    fn deactivate_removes_jumps_then_chains() {
        let f = fake("", true);
        deactivate(&f, ONCE, V4_CHAINS).unwrap();
        let calls = f.calls.lock().unwrap();
        assert!(calls.contains(&"-t nat -D OUTPUT -j PORTMASTER-REDIRECT".to_string()));
        assert!(calls.contains(&"-t nat -X PORTMASTER-REDIRECT".to_string()));
    }

    #[test]
    fn displaced_jump_is_moved_with_temporary_rule() {
        let f = fake(
            "-P OUTPUT ACCEPT\n-A OUTPUT -j DOCKER\n-A OUTPUT -j PORTMASTER-FILTER\n",
            true,
        );
        let moved = reinsert_displaced(&f, &["filter OUTPUT -j PORTMASTER-FILTER"]).unwrap();
        assert_eq!(moved, vec!["filter OUTPUT -j PORTMASTER-FILTER"]);
        let calls = f.calls.lock().unwrap();
        let expect = [
            "-t filter -I OUTPUT 1 -m comment --comment TEMPORARY_RULE -j PORTMASTER-FILTER",
            "-t filter -D OUTPUT -j PORTMASTER-FILTER",
            "-t filter -I OUTPUT 1 -j PORTMASTER-FILTER",
            "-t filter -D OUTPUT -m comment --comment TEMPORARY_RULE -j PORTMASTER-FILTER",
        ];
        let tail: Vec<&String> = calls
            .iter()
            .filter(|c| !c.contains(" -S") && !c.contains(" -C "))
            .collect();
        assert_eq!(tail.len(), expect.len());
        for (got, want) in tail.iter().zip(expect) {
            assert_eq!(got.as_str(), want);
        }
    }

    #[test]
    fn rules_are_go_verbatim() {
        // Rule counts and queue numbers as in Go.
        assert_eq!(V4_RULES.len(), 28);
        assert_eq!(V6_RULES.len(), 28);
        assert!(V6_RULES.iter().any(|r| r.contains("--queue-num 17160")));
        assert!(V4_RULES.iter().all(|r| !r.contains("icmpv6")));
    }
}
