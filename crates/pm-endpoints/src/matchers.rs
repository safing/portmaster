//! Rule matching, port of the Go `Matches` methods.

use std::collections::BTreeMap;

use pm_intel::Entity;
use pm_netstate::IpScope;

use crate::endpoint::{
    scope_names, DomainMatchType, Endpoint, EndpointKind, SCOPE_INTERNET, SCOPE_LAN,
    SCOPE_LOCALHOST,
};
use crate::{EpResult, Reason};

type Match = (EpResult, Option<Reason>);

impl Endpoint {
    /// Matches the entity against this rule.
    pub fn matches(&self, entity: &mut Entity) -> Match {
        match &self.kind {
            EndpointKind::Any => self.result(entity, "*".to_string(), "matches", None),
            EndpointKind::Ip(ip) => match entity.ip {
                Some(eip) if eip.to_canonical() == *ip => {
                    self.result(entity, ip.to_string(), "IP matches", None)
                }
                _ => no_match(),
            },
            EndpointKind::IpRange(net) => match entity.ip {
                Some(eip) if net.contains(eip) => {
                    self.result(entity, net.to_string(), "IP is in", None)
                }
                _ => no_match(),
            },
            EndpointKind::Country(code) => {
                if !global_ip(entity) {
                    return no_match();
                }
                let Some(info) = entity.get_country_info() else {
                    return self.without_data(
                        entity,
                        String::new(),
                        "country data not available to match",
                    );
                };
                if *code == info.code {
                    let value = format!("{} ({})", info.name, info.code);
                    self.result(entity, value, "IP is located in", None)
                } else {
                    no_match()
                }
            }
            EndpointKind::Continent(code) => {
                if !global_ip(entity) {
                    return no_match();
                }
                let Some(info) = entity.get_country_info() else {
                    return self.without_data(
                        entity,
                        String::new(),
                        "country data not available to match",
                    );
                };
                if *code == info.continent.code {
                    let value = format!("{} ({})", info.continent.name, info.continent.code);
                    self.result(entity, value, "IP is located in", None)
                } else {
                    no_match()
                }
            }
            EndpointKind::Asn(asn) => {
                if !global_ip(entity) {
                    return no_match();
                }
                let (entity_asn, ok) = entity.get_asn();
                if !ok {
                    return self.without_data(
                        entity,
                        crate::endpoint::go_int(*asn).to_string(),
                        "ASN data not available to match",
                    );
                }
                if u64::from(entity_asn) == *asn {
                    self.result(
                        entity,
                        crate::endpoint::go_int(*asn).to_string(),
                        "IP is part of AS",
                        None,
                    )
                } else {
                    no_match()
                }
            }
            EndpointKind::Scope(scopes) => {
                if entity.ip.is_none() {
                    return no_match();
                }
                let scope = match entity.ip_scope {
                    IpScope::HostLocal => SCOPE_LOCALHOST,
                    IpScope::LinkLocal | IpScope::SiteLocal | IpScope::LocalMulticast => SCOPE_LAN,
                    IpScope::Global | IpScope::GlobalMulticast => SCOPE_INTERNET,
                    IpScope::Undefined | IpScope::Invalid => return no_match(),
                };
                if scopes & scope > 0 {
                    self.result(entity, scope_names(*scopes), "scope matches", None)
                } else {
                    no_match()
                }
            }
            EndpointKind::Lists { list_set, lists } => {
                if entity.match_lists(list_set) {
                    let extra = ("filterlist", entity.list_block_reason().to_json());
                    self.result(entity, lists.clone(), "filterlist contains", Some(extra))
                } else {
                    no_match()
                }
            }
            EndpointKind::Domain { .. } => self.match_domain(entity),
        }
    }

    fn match_domain(&self, entity: &mut Entity) -> Match {
        let (domain, ok) = entity.get_domain(true);
        if !ok {
            return no_match();
        }
        let result = self.check_domain(entity, &domain);
        if result.0 != EpResult::NoMatch {
            return result;
        }
        if entity.cname_check_enabled() {
            for cname in &entity.cname {
                let result = self.check_domain(entity, cname);
                if result.0 == EpResult::Denied {
                    return result;
                }
            }
        }
        no_match()
    }

    fn check_domain(&self, entity: &Entity, candidate: &str) -> Match {
        let EndpointKind::Domain {
            original_value,
            domain,
            domain_zone,
            match_type,
        } = &self.kind
        else {
            return no_match();
        };
        let matched = match match_type {
            DomainMatchType::Exact => candidate == domain,
            DomainMatchType::Zone => {
                candidate == domain || candidate.ends_with(domain_zone.as_str())
            }
            DomainMatchType::Suffix => candidate.ends_with(domain.as_str()),
            DomainMatchType::Prefix => candidate.starts_with(domain.as_str()),
            DomainMatchType::Contains => candidate.contains(domain.as_str()),
        };
        if matched {
            self.result(entity, original_value.clone(), "domain matches", None)
        } else {
            no_match()
        }
    }

    /// Go `matchesPPP`: protocol and port filter, then the rule verdict.
    fn ppp_result(&self, entity: &Entity) -> EpResult {
        if self.protocol > 0 && entity.protocol != self.protocol {
            return EpResult::NoMatch;
        }
        if self.start_port > 0 {
            let port = entity.dst_port();
            if port < self.start_port || port > self.end_port {
                return EpResult::NoMatch;
            }
        }
        if self.permitted {
            EpResult::Permitted
        } else {
            EpResult::Denied
        }
    }

    /// Go `match`: applies the PPP filter and builds the reason.
    fn result(
        &self,
        entity: &Entity,
        value: String,
        description: &str,
        extra: Option<(&str, serde_json::Value)>,
    ) -> Match {
        let result = self.ppp_result(entity);
        if result == EpResult::NoMatch {
            return no_match();
        }
        (result, Some(self.reason(value, description, extra)))
    }

    /// Go `matchWithoutData`: a rule whose data is missing.
    fn without_data(&self, entity: &Entity, value: String, description: &str) -> Match {
        if self.ppp_result(entity) == EpResult::NoMatch {
            return no_match();
        }
        // An allow rule that cannot be evaluated is skipped; a deny rule fails safe.
        if self.permitted {
            return no_match();
        }
        (
            EpResult::MatchError,
            Some(self.reason(value, description, None)),
        )
    }

    fn reason(
        &self,
        value: String,
        description: &str,
        extra: Option<(&str, serde_json::Value)>,
    ) -> Reason {
        let mut extras = BTreeMap::new();
        if let Some((key, val)) = extra {
            extras.insert(key.to_string(), val);
        }
        Reason {
            description: description.to_string(),
            filter: self.to_string(),
            value,
            permitted: self.permitted,
            extra: extras,
        }
    }
}

fn global_ip(entity: &Entity) -> bool {
    entity.ip.is_some() && entity.ip_scope.is_global()
}

fn no_match() -> Match {
    (EpResult::NoMatch, None)
}
