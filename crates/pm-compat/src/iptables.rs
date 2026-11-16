//! iptables chain enumeration, port of Go `service/compat/iptables.go`
//! (`GetIPTablesChains`).
//!
//! Chain listing goes through the `pm_interception::iptables::Runner` trait so
//! tests can supply a fake command runner instead of calling `iptables`.

use pm_interception::iptables::{Cmd, Runner};

/// IP family, matching Go `iptables.ProtocolIPv4/IPv6`.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Protocol {
    V4,
    V6,
}

/// Lists the chain names of one table. Injectable for tests.
pub trait ChainLister: Send + Sync {
    fn list_chains(&self, protocol: Protocol, table: &str) -> Result<Vec<String>, String>;
}

/// Tables queried by Go, in order.
pub const IPT_TABLES: &[&str] = &["filter", "nat", "mangle", "raw"];

/// `ChainLister` backed by the real `iptables`/`ip6tables` binaries.
pub struct SystemChainLister {
    v4: Cmd,
    v6: Cmd,
}

impl Default for SystemChainLister {
    fn default() -> Self {
        Self::new()
    }
}

impl SystemChainLister {
    pub fn new() -> SystemChainLister {
        SystemChainLister {
            v4: Cmd::v4(),
            v6: Cmd::v6(),
        }
    }
}

impl ChainLister for SystemChainLister {
    fn list_chains(&self, protocol: Protocol, table: &str) -> Result<Vec<String>, String> {
        let runner: &dyn Runner = match protocol {
            Protocol::V4 => &self.v4,
            Protocol::V6 => &self.v6,
        };
        // go-iptables `ListChains` runs `-t <table> -L -n` and parses lines that
        // start with "Chain ".
        let output = runner
            .run(&["-t", table, "-L", "-n"])
            .map_err(|error| error.to_string())?;
        Ok(parse_chains(&output))
    }
}

fn parse_chains(output: &str) -> Vec<String> {
    output
        .lines()
        .filter_map(|line| {
            line.strip_prefix("Chain ")?
                .split_whitespace()
                .next()
                .map(str::to_string)
        })
        .collect()
}

/// Pure formatting of the chain tree: `v4`/`v6`, two-space table names and
/// four-space chain names, like Go `GetIPTablesChains`.
pub fn get_iptables_chains(lister: &dyn ChainLister) -> Result<Vec<String>, String> {
    let mut chains = Vec::with_capacity(100);
    for (protocol, label) in [(Protocol::V4, "v4"), (Protocol::V6, "v6")] {
        chains.push(label.to_string());
        for table in IPT_TABLES {
            chains.push(format!("  {table}"));
            for name in lister.list_chains(protocol, table)? {
                chains.push(format!("    {name}"));
            }
        }
    }
    Ok(chains)
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::sync::Mutex;

    struct Fake {
        calls: Mutex<Vec<(Protocol, String)>>,
        chains: Vec<String>,
        fail_table: Option<String>,
    }

    impl ChainLister for Fake {
        fn list_chains(&self, protocol: Protocol, table: &str) -> Result<Vec<String>, String> {
            self.calls
                .lock()
                .unwrap()
                .push((protocol, table.to_string()));
            if self.fail_table.as_deref() == Some(table) {
                return Err(format!("failed to get chains of table {table}: denied"));
            }
            Ok(self.chains.clone())
        }
    }

    #[test]
    fn parses_go_style_chain_listing() {
        let listing = "Chain INPUT (policy ACCEPT)\nChain FORWARD (policy ACCEPT)\nChain DOCKER (1 references)\n";
        assert_eq!(
            parse_chains(listing),
            vec!["INPUT", "FORWARD", "DOCKER"]
        );
    }

    #[test]
    fn formats_full_chain_tree() {
        let fake = Fake {
            calls: Mutex::new(Vec::new()),
            chains: vec!["PORTMASTER-FILTER".to_string()],
            fail_table: None,
        };
        let chains = get_iptables_chains(&fake).unwrap();
        assert_eq!(chains[0], "v4");
        assert_eq!(chains[1], "  filter");
        assert_eq!(chains[2], "    PORTMASTER-FILTER");
        assert_eq!(chains[3], "  nat");
        // 2 families x (1 label + 4 tables x (1 label + 1 chain)) = 18 lines.
        assert_eq!(chains.len(), 18);
        assert_eq!(fake.calls.lock().unwrap().len(), 8);
    }

    #[test]
    fn propagates_list_errors() {
        let fake = Fake {
            calls: Mutex::new(Vec::new()),
            chains: Vec::new(),
            fail_table: Some("nat".to_string()),
        };
        let error = get_iptables_chains(&fake).unwrap_err();
        assert_eq!(
            error,
            "failed to get chains of table nat: denied"
        );
    }
}
