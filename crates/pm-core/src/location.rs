//! Core environment for device location estimation.
//!
//! Implements `pm_geoip::LocationEnvironment` with the netenv caches, the
//! GeoIP reader, an ICMP traceroute and the local UTC offset. Everything that
//! needs privileges or machine-specific data is guarded: failures return
//! "not available" and the location service falls back to the timezone.

use std::net::IpAddr;
use std::process::Command;

use pm_geoip::{DeviceLocationService, Location, LocationEnvironment};
use pm_netstate::{ip_scope, IpScope};

/// Device location service configured by the core.
pub type CoreDeviceLocations = DeviceLocationService<CoreLocationEnvironment>;

/// Target of the traceroute, like Go's `locationTestingIPv4`.
pub const LOCATION_TEST_IP: IpAddr = IpAddr::V4(std::net::Ipv4Addr::new(1, 1, 1, 1));

/// Inputs of the device location service supplied by the core.
pub struct CoreLocationEnvironment {
    addresses: std::sync::Arc<pm_netenv::AddressCache>,
    geoip: std::sync::Arc<pm_geoip::GeoIp>,
    flag: pm_netenv::NetworkChangeFlag,
    traceroute_allowed: bool,
}

impl CoreLocationEnvironment {
    /// Creates the environment.
    pub fn new(
        addresses: std::sync::Arc<pm_netenv::AddressCache>,
        geoip: std::sync::Arc<pm_geoip::GeoIp>,
        flag: pm_netenv::NetworkChangeFlag,
    ) -> CoreLocationEnvironment {
        CoreLocationEnvironment {
            addresses,
            geoip,
            flag,
            traceroute_allowed: is_root(),
        }
    }

    /// Creates the service backed by this environment.
    pub fn into_service(self) -> DeviceLocationService<CoreLocationEnvironment> {
        DeviceLocationService::new(self)
    }
}

impl LocationEnvironment for CoreLocationEnvironment {
    fn network_changed(&self) -> bool {
        self.flag.is_set()
    }

    fn refresh_network_changed(&self) {
        self.flag.refresh();
    }

    fn assigned_global_addresses(&self) -> (Vec<IpAddr>, Vec<IpAddr>) {
        match self.addresses.get_assigned_global_addresses() {
            Ok((ipv4, ipv6)) => (
                ipv4.into_iter().map(IpAddr::V4).collect(),
                ipv6.into_iter().map(IpAddr::V6).collect(),
            ),
            Err(error) => {
                tracing::warn!(%error, "netenv: failed to get global addresses for location");
                (Vec::new(), Vec::new())
            }
        }
    }

    fn ipv6_enabled(&self) -> bool {
        self.addresses
            .get_assigned_addresses()
            .map(|(_, ipv6)| !ipv6.is_empty())
            .unwrap_or(false)
    }

    fn get_location(&self, ip: IpAddr) -> Option<Location> {
        self.geoip.get_location(ip).ok()
    }

    fn traceroute(&self) -> Option<IpAddr> {
        if !self.traceroute_allowed {
            return None;
        }
        let socket = pm_netenv::SystemIcmpSocket::new().ok()?;
        match pm_netenv::run_probe(&socket, LOCATION_TEST_IP, std::process::id() as u16, |ip| {
            ip_scope(ip) == IpScope::Global
        }) {
            Ok(pm_netenv::ProbeOutcome::FoundRouter(ip)) => Some(ip),
            Ok(other) => {
                tracing::debug!("netenv: traceroute for location found nothing: {other:?}");
                None
            }
            Err(error) => {
                tracing::warn!(%error, "netenv: traceroute for location failed");
                None
            }
        }
    }

    fn timezone_offset_seconds(&self) -> i32 {
        timezone_offset_seconds()
    }
}

/// Returns the local UTC offset in seconds, like `time.Now().Zone()`.
///
/// Uses `date +%z` so no `unsafe` is needed; a failure yields UTC.
pub(crate) fn timezone_offset_seconds() -> i32 {
    let output = Command::new("date").arg("+%z").output().ok();
    output
        .and_then(|output| {
            let text = String::from_utf8(output.stdout).ok()?;
            parse_timezone_offset(text.trim())
        })
        .unwrap_or(0)
}

/// Parses a `+HHMM` / `-HHMM` UTC offset.
fn parse_timezone_offset(value: &str) -> Option<i32> {
    if value.len() != 5 {
        return None;
    }
    let sign = match &value[0..1] {
        "+" => 1,
        "-" => -1,
        _ => return None,
    };
    let hours: i32 = value[1..3].parse().ok()?;
    let minutes: i32 = value[3..5].parse().ok()?;
    if hours > 23 || minutes > 59 {
        return None;
    }
    Some(sign * (hours * 3600 + minutes * 60))
}

#[cfg(target_os = "linux")]
fn is_root() -> bool {
    nix::unistd::geteuid().is_root()
}

#[cfg(not(target_os = "linux"))]
fn is_root() -> bool {
    false
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parses_timezone_offsets() {
        assert_eq!(parse_timezone_offset("+0300"), Some(3 * 3600));
        assert_eq!(parse_timezone_offset("-0500"), Some(-5 * 3600));
        assert_eq!(parse_timezone_offset("+0530"), Some(5 * 3600 + 30 * 60));
        assert_eq!(parse_timezone_offset("+0000"), Some(0));
        assert_eq!(parse_timezone_offset("+2400"), None);
        assert_eq!(parse_timezone_offset("03:00"), None);
        assert_eq!(parse_timezone_offset(""), None);
    }
}
