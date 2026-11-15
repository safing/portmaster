//! Port of Go `service/network/netutils.GetIPScope` (the part used by lookups).

use std::net::{IpAddr, Ipv4Addr, Ipv6Addr};

use serde::{Deserialize, Deserializer, Serialize, Serializer};

/// The network scope of an IP address. Discriminants match Go `netutils.IPScope`.
#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord, Hash, Default)]
#[repr(i8)]
pub enum IpScope {
    Invalid = -1,
    #[default]
    Undefined = 0,
    HostLocal = 1,
    LinkLocal = 2,
    SiteLocal = 3,
    Global = 4,
    LocalMulticast = 5,
    GlobalMulticast = 6,
}

impl IpScope {
    /// Returns true if the scope refers to the host itself.
    pub fn is_localhost(self) -> bool {
        self == Self::HostLocal
    }

    /// Returns true if the scope is site-local, link-local or local multicast.
    pub fn is_lan(self) -> bool {
        matches!(self, Self::SiteLocal | Self::LinkLocal | Self::LocalMulticast)
    }

    /// Returns true if the scope is global.
    pub fn is_global(self) -> bool {
        matches!(self, Self::Global | Self::GlobalMulticast)
    }
}

impl Serialize for IpScope {
    fn serialize<S: Serializer>(&self, serializer: S) -> Result<S::Ok, S::Error> {
        serializer.serialize_i8(*self as i8)
    }
}

impl<'de> Deserialize<'de> for IpScope {
    fn deserialize<D: Deserializer<'de>>(deserializer: D) -> Result<Self, D::Error> {
        Ok(match i8::deserialize(deserializer)? {
            0 => Self::Undefined,
            1 => Self::HostLocal,
            2 => Self::LinkLocal,
            3 => Self::SiteLocal,
            4 => Self::Global,
            5 => Self::LocalMulticast,
            6 => Self::GlobalMulticast,
            _ => Self::Invalid,
        })
    }
}

/// Returns the network scope of the given IP address.
pub fn ip_scope(ip: IpAddr) -> IpScope {
    match ip {
        IpAddr::V4(ip) => scope_v4(ip),
        IpAddr::V6(ip) => scope_v6(ip),
    }
}

fn scope_v4(ip: Ipv4Addr) -> IpScope {
    let octets = ip.octets();
    match octets {
        // 0.0.0.0/32 is used as source for L2 protocols without L3 addressing.
        [0, 0, 0, 0] => IpScope::LocalMulticast,
        [0, _, _, _] => IpScope::Invalid,
        [10, _, _, _] => IpScope::SiteLocal,
        [100, b, _, _] if b & 0b1100_0000 == 64 => IpScope::SiteLocal,
        [127, _, _, _] => IpScope::HostLocal,
        [169, 254, _, _] => IpScope::LinkLocal,
        [172, b, _, _] if b & 0b1111_0000 == 16 => IpScope::SiteLocal,
        [192, 0, 2, _] => IpScope::Invalid,
        [192, 168, _, _] => IpScope::SiteLocal,
        [198, 51, 100, _] => IpScope::Invalid,
        [203, 0, 113, _] => IpScope::Invalid,
        [224, _, _, _] => IpScope::LocalMulticast,
        [233, 252, 0, _] => IpScope::Invalid,
        [225..=238, _, _, _] => IpScope::GlobalMulticast,
        [239, _, _, _] => IpScope::LocalMulticast,
        [255, 255, 255, 255] => IpScope::LocalMulticast,
        [240..=255, _, _, _] => IpScope::Invalid,
        _ => IpScope::Global,
    }
}

fn scope_v6(ip: Ipv6Addr) -> IpScope {
    let octets = ip.octets();
    match octets {
        [0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0] => IpScope::Invalid,
        [0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1] => IpScope::HostLocal,
        [b0, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _] if b0 & 0xfe == 0xfc => {
            IpScope::SiteLocal
        }
        [0xfe, b1, _, _, _, _, _, _, _, _, _, _, _, _, _, _] if b1 & 0xc0 == 0x80 => {
            IpScope::LinkLocal
        }
        [0xff, b1, _, _, _, _, _, _, _, _, _, _, _, _, _, _] if b1 <= 0x05 => {
            IpScope::LocalMulticast
        }
        [0xff, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _] => IpScope::GlobalMulticast,
        _ => IpScope::Global,
    }
}

/// Returns whether the IP is a local multicast or broadcast address.
pub fn is_local_multicast(ip: IpAddr) -> bool {
    ip_scope(ip) == IpScope::LocalMulticast
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn discriminants_and_serde_match_go() {
        let all = [
            (IpScope::Invalid, -1),
            (IpScope::Undefined, 0),
            (IpScope::HostLocal, 1),
            (IpScope::LinkLocal, 2),
            (IpScope::SiteLocal, 3),
            (IpScope::Global, 4),
            (IpScope::LocalMulticast, 5),
            (IpScope::GlobalMulticast, 6),
        ];
        for (scope, n) in all {
            assert_eq!(scope as i8, n);
            assert_eq!(serde_json::to_string(&scope).unwrap(), n.to_string());
            assert_eq!(serde_json::from_str::<IpScope>(&n.to_string()).unwrap(), scope);
        }
        assert!(IpScope::LinkLocal.is_lan() && !IpScope::Global.is_lan());
        assert!(IpScope::GlobalMulticast.is_global() && !IpScope::HostLocal.is_global());
        assert!(IpScope::HostLocal.is_localhost());
    }

    #[test]
    fn ipv4_scopes() {
        let scope = |ip: &str| ip_scope(ip.parse().unwrap());
        assert_eq!(scope("0.0.0.0"), IpScope::LocalMulticast);
        assert_eq!(scope("0.1.2.3"), IpScope::Invalid);
        assert_eq!(scope("10.0.0.1"), IpScope::SiteLocal);
        assert_eq!(scope("100.64.1.1"), IpScope::SiteLocal);
        assert_eq!(scope("127.0.0.1"), IpScope::HostLocal);
        assert_eq!(scope("169.254.1.1"), IpScope::LinkLocal);
        assert_eq!(scope("172.16.0.1"), IpScope::SiteLocal);
        assert_eq!(scope("172.32.0.1"), IpScope::Global);
        assert_eq!(scope("192.168.1.1"), IpScope::SiteLocal);
        assert_eq!(scope("1.1.1.1"), IpScope::Global);
        assert_eq!(scope("224.0.0.1"), IpScope::LocalMulticast);
        assert_eq!(scope("232.0.0.1"), IpScope::GlobalMulticast);
        assert_eq!(scope("239.255.255.250"), IpScope::LocalMulticast);
        assert_eq!(scope("255.255.255.255"), IpScope::LocalMulticast);
        assert_eq!(scope("240.0.0.1"), IpScope::Invalid);
    }

    #[test]
    fn ipv6_scopes() {
        let scope = |ip: &str| ip_scope(ip.parse().unwrap());
        assert_eq!(scope("::"), IpScope::Invalid);
        assert_eq!(scope("::1"), IpScope::HostLocal);
        assert_eq!(scope("fd00::1"), IpScope::SiteLocal);
        assert_eq!(scope("fe80::1"), IpScope::LinkLocal);
        assert_eq!(scope("ff02::1"), IpScope::LocalMulticast);
        assert_eq!(scope("ff05::1"), IpScope::LocalMulticast);
        assert_eq!(scope("ff0e::1"), IpScope::GlobalMulticast);
        assert_eq!(scope("2001:db8::1"), IpScope::Global);
    }
}
