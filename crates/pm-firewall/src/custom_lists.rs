//! Adapts the custom filter list service to the firewall trait.

use std::sync::Arc;

use pm_intel::customlists::CustomListService;

use crate::CustomLists;

/// [`CustomLists`] backed by the updateable custom filter list.
pub struct ServiceCustomLists {
    service: Arc<CustomListService>,
}

impl ServiceCustomLists {
    /// Creates the adapter.
    pub fn new(service: Arc<CustomListService>) -> ServiceCustomLists {
        ServiceCustomLists { service }
    }

    /// Returns the underlying service.
    pub fn service(&self) -> &Arc<CustomListService> {
        &self.service
    }
}

impl CustomLists for ServiceCustomLists {
    fn is_loaded(&self) -> bool {
        self.service.lists().is_loaded()
    }

    fn lookup_domain(&self, fqdn: &str, filter_subdomains: bool) -> (bool, String) {
        self.service
            .lists()
            .lookup_domain(fqdn, filter_subdomains)
    }

    fn lookup_ip(&self, ip: std::net::IpAddr) -> bool {
        self.service.lists().lookup_ip(ip)
    }

    fn lookup_asn(&self, asn: u32) -> bool {
        self.service.lists().lookup_asn(asn)
    }

    fn lookup_country(&self, country: &str) -> bool {
        self.service.lists().lookup_country(country)
    }
}
