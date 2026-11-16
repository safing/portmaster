//! Compatibility section for the debug output, port of Go
//! `service/compat/debug_linux.go`.

use pm_debug::{Info, ADD_CONTENT_LINE_BREAKS, USE_CODE_SECTION};

use crate::iptables::{get_iptables_chains, ChainLister};

/// Adds the iptables-chain section to the debug info.
///
/// Ten of the formatted lines are group headers (`v4`, `v6` and eight table
/// names), so the chain count subtracts them, like Go `len(chains)-10`.
pub fn add_to_debug_info(di: &mut Info, lister: &dyn ChainLister) {
    match get_iptables_chains(lister) {
        Ok(chains) => {
            let count = chains.len().saturating_sub(10);
            let parts: Vec<&str> = chains.iter().map(String::as_str).collect();
            di.add_section(
                &format!("Compatibility: IPTables Chains ({count})"),
                USE_CODE_SECTION | ADD_CONTENT_LINE_BREAKS,
                &parts,
            );
        }
        Err(error) => {
            di.add_section(
                "Compatibility: IPTables Chains (failed)",
                USE_CODE_SECTION,
                &[&error],
            );
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    struct Lister {
        result: Result<Vec<String>, String>,
    }

    impl ChainLister for Lister {
        fn list_chains(&self, _protocol: crate::iptables::Protocol, _table: &str) -> Result<Vec<String>, String> {
            self.result.clone()
        }
    }

    #[test]
    fn adds_chain_section_with_count() {
        let mut di = Info::new();
        add_to_debug_info(
            &mut di,
            &Lister {
                result: Ok(vec!["PORTMASTER-FILTER".to_string(); 8]),
            },
        );
        let text = di.as_str();
        // 8 chains per table x 8 (protocol, table) pairs.
        assert!(text.contains("**Compatibility: IPTables Chains (64)**:"));
        assert!(text.contains("PORTMASTER-FILTER"));
    }

    #[test]
    fn failed_listing_is_reported_without_panicking() {
        let mut di = Info::new();
        add_to_debug_info(
            &mut di,
            &Lister {
                result: Err("iptables: permission denied".to_string()),
            },
        );
        let text = di.as_str();
        assert!(text.contains("**Compatibility: IPTables Chains (failed)**:"));
        assert!(text.contains("iptables: permission denied"));
    }
}
