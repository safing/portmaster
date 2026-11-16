//! Operating system interface enumeration, port of the platform parts of Go
//! `service/netenv/interfaces_linux.go` and `interfaces_default.go`.
//!
//! Linux uses `getifaddrs` for the interfaces, `/proc/net/route` and
//! `/proc/net/ipv6_route` for the default routes, and `/sys/class/net/<name>/device`
//! to recognize physical adapters. Other platforms have no system interface
//! source yet: enumeration returns an empty list and physical/default-route
//! detection reports "unsupported". Go's Windows interface enumeration
//! (`GetAdaptersAddresses`) is not ported because it requires Windows-only
//! unsafe FFI; the PowerShell-based nameserver/gateway lookup lives in the
//! private `windows_env` module.

#[cfg(unix)]
use std::collections::BTreeMap;
#[cfg(unix)]
use std::net::IpAddr;

use crate::interfaces::{RawInterface, RouteCandidate, InterfaceSource};
#[cfg(unix)]
use crate::interfaces::{mask_prefix_v4, mask_prefix_v6};

/// Operating system interface source.
#[derive(Debug, Default, Clone, Copy)]
pub struct SystemInterfaces;

impl InterfaceSource for SystemInterfaces {
    #[cfg(unix)]
    fn interfaces(&self) -> Result<Vec<RawInterface>, String> {
        let addrs = nix::ifaddrs::getifaddrs().map_err(|error| error.to_string())?;
        let mut interfaces: BTreeMap<String, RawInterface> = BTreeMap::new();

        for addr in addrs {
            let name = addr.interface_name.clone();
            let flags = addr.flags;
            let entry = interfaces.entry(name.clone()).or_insert_with(|| RawInterface {
                name: name.clone(),
                index: nix::net::if_::if_nametoindex(name.as_str()).unwrap_or(0),
                mac: None,
                up: flags.contains(nix::net::if_::InterfaceFlags::IFF_UP),
                loopback: flags.contains(nix::net::if_::InterfaceFlags::IFF_LOOPBACK),
                addrs: Vec::new(),
            });

            let Some(address) = addr.address else {
                continue;
            };

            if let Some(inet) = address.as_sockaddr_in() {
                let ip = IpAddr::V4(inet.ip());
                let prefix = addr
                    .netmask
                    .as_ref()
                    .and_then(|mask| mask.as_sockaddr_in())
                    .map_or(32, |mask| mask_prefix_v4(mask.ip()));
                entry.addrs.push((ip, prefix));
            } else if let Some(inet6) = address.as_sockaddr_in6() {
                let ip = IpAddr::V6(inet6.ip());
                let prefix = addr
                    .netmask
                    .as_ref()
                    .and_then(|mask| mask.as_sockaddr_in6())
                    .map_or(128, |mask| mask_prefix_v6(mask.ip()));
                entry.addrs.push((ip, prefix));
            }
        }

        // Hardware addresses are only available through sysfs on Linux.
        #[cfg(target_os = "linux")]
        for entry in interfaces.values_mut() {
            if entry.mac.is_some() {
                continue;
            }
            if let Ok(mac) = std::fs::read_to_string(format!(
                "/sys/class/net/{}/address",
                entry.name
            )) {
                let mac = mac.trim().to_lowercase();
                if mac.len() == 17 {
                    entry.mac = Some(mac);
                }
            }
        }

        Ok(interfaces.into_values().collect())
    }

    /// Interface enumeration uses `getifaddrs`, which is only available on
    /// Unix. Go's Windows path calls `GetAdaptersAddresses` through P/Invoke;
    /// it needs unsafe FFI and platform types that are not part of this port,
    /// so until it lands the system source simply has no interfaces to report.
    #[cfg(not(unix))]
    fn interfaces(&self) -> Result<Vec<RawInterface>, String> {
        Ok(Vec::new())
    }

    fn is_physical(&self, name: &str) -> bool {
        #[cfg(target_os = "linux")]
        {
            std::path::Path::new(&format!("/sys/class/net/{name}/device")).exists()
        }
        #[cfg(not(target_os = "linux"))]
        {
            let _ = name;
            false
        }
    }

    fn ipv4_default_routes(&self) -> Result<Vec<RouteCandidate>, String> {
        #[cfg(target_os = "linux")]
        {
            let data = std::fs::read_to_string("/proc/net/route")
                .map_err(|error| error.to_string())?;
            let mut routes = Vec::new();
            // Columns: Iface Destination Gateway Flags RefCnt Use Metric Mask ...
            for line in data.lines().skip(1) {
                let fields: Vec<&str> = line.split_whitespace().collect();
                if fields.len() < 8 {
                    continue;
                }
                let Some(destination) = parse_le_hex(fields[1]) else {
                    continue;
                };
                let Some(mask) = parse_le_hex(fields[7]) else {
                    continue;
                };
                if destination != 0 || mask != 0 {
                    continue;
                }
                let Ok(metric) = fields[6].parse::<u32>() else {
                    continue;
                };
                routes.push(RouteCandidate {
                    interface: fields[0].to_string(),
                    metric,
                });
            }
            Ok(routes)
        }
        #[cfg(not(target_os = "linux"))]
        {
            Err("physical network interface detection is not supported on this platform"
                .to_string())
        }
    }

    fn ipv6_default_routes(&self) -> Vec<RouteCandidate> {
        #[cfg(target_os = "linux")]
        {
            let Ok(data) = std::fs::read_to_string("/proc/net/ipv6_route") else {
                // IPv6 may simply not be configured.
                return Vec::new();
            };
            let mut routes = Vec::new();
            // Columns: dest destpfxlen src srcpfxlen nexthop metric refcnt use flags iface
            for line in data.lines() {
                let fields: Vec<&str> = line.split_whitespace().collect();
                if fields.len() < 10 {
                    continue;
                }
                if fields[0] != "00000000000000000000000000000000" {
                    continue;
                }
                if fields[1] != "00" && fields[1].parse::<u8>().ok() != Some(0) {
                    continue;
                }
                // Skip on-link entries without a gateway.
                if fields[4] == "00000000000000000000000000000000" {
                    continue;
                }
                let Ok(metric) = u32::from_str_radix(fields[5], 16) else {
                    continue;
                };
                routes.push(RouteCandidate {
                    interface: fields[fields.len() - 1].to_string(),
                    metric,
                });
            }
            routes
        }
        #[cfg(not(target_os = "linux"))]
        {
            Vec::new()
        }
    }
}

/// Parses a little-endian 32-bit value from eight hex characters.
#[cfg(target_os = "linux")]
fn parse_le_hex(value: &str) -> Option<u32> {
    if value.len() != 8 {
        return None;
    }
    let bytes = (0..4)
        .map(|index| u8::from_str_radix(&value[index * 2..index * 2 + 2], 16).ok())
        .collect::<Option<Vec<u8>>>()?;
    Some(u32::from_le_bytes([bytes[0], bytes[1], bytes[2], bytes[3]]))
}
