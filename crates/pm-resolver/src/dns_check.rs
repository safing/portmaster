//! A DNS connectivity check for the online status monitor, port of Go's
//! `DNSTestQueryFunc`.
//!
//! The monitor runs its checks on a blocking thread, while the resolver is
//! async. [`ResolverDnsCheck`] bridges the two by driving the query on the
//! Tokio runtime that was current when the check was created.

use std::sync::Arc;

use pm_netenv::{DnsCheck, DnsCheckOutcome};
use tokio::runtime::Handle;
use tokio_util::sync::CancellationToken;

use crate::ResolverService;

/// Resolves the online-status test domain through the Portmaster resolver.
pub struct ResolverDnsCheck {
    resolver: ResolverService,
    handle: Handle,
}

impl ResolverDnsCheck {
    /// Wraps the resolver. Create this from an async context so the runtime
    /// handle is available when the monitor calls [`DnsCheck::query`] from its
    /// blocking worker.
    pub fn new(resolver: ResolverService) -> ResolverDnsCheck {
        ResolverDnsCheck {
            resolver,
            handle: Handle::current(),
        }
    }
}

impl ResolverService {
    /// Returns the DNS connectivity callback for the online status monitor,
    /// like Go registering `SetDNSTestQueryFunc`.
    pub fn dns_check(&self) -> Arc<dyn DnsCheck> {
        Arc::new(ResolverDnsCheck::new(self.clone()))
    }
}

impl DnsCheck for ResolverDnsCheck {
    fn query(&self, fqdn: &str) -> DnsCheckOutcome {
        let cancel = CancellationToken::new();
        let (ips, answered, error) = self
            .handle
            .block_on(self.resolver.test_connectivity(fqdn, None, &cancel));

        if !answered {
            return DnsCheckOutcome::Failed(
                error
                    .map(|error| error.to_string())
                    .unwrap_or_else(|| "dns query did not complete".to_string()),
            );
        }

        match error {
            None => DnsCheckOutcome::Answered(ips),
            // A negative answer is acceptable for the connectivity check, like
            // Go's `ErrNotFound` special case.
            Some(error) if error.is_not_found() => {
                DnsCheckOutcome::AnsweredWithError(error.to_string())
            }
            // A blocked upstream means DNS is not usable, even though the
            // query itself was answered.
            Some(error) if error.is_blocked() => DnsCheckOutcome::Failed(error.to_string()),
            Some(error) => DnsCheckOutcome::AnsweredWithError(error.to_string()),
        }
    }
}
