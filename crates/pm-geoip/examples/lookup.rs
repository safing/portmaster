//! Looks up an IP address in a MaxMind database.
//!
//! ```text
//! PM_GEOIP_V4=/var/lib/portmaster/intel/geoipv4.mmdb \
//!   cargo run -p pm-geoip --example lookup -- 93.184.216.34
//! ```

use std::net::IpAddr;

fn main() {
    let arguments: Vec<String> = std::env::args().skip(1).collect();
    let ip: IpAddr = match arguments.first() {
        Some(value) => value.parse().expect("a valid IP address"),
        None => {
            eprintln!("usage: lookup <ip>");
            std::process::exit(2);
        }
    };

    let path = if ip.is_ipv6() {
        std::env::var("PM_GEOIP_V6")
    } else {
        std::env::var("PM_GEOIP_V4")
    };
    let path = path.unwrap_or_else(|_| {
        eprintln!("PM_GEOIP_V4/PM_GEOIP_V6 must point to a MaxMind database");
        std::process::exit(2);
    });

    let geoip = pm_geoip::GeoIp::new();
    geoip
        .replace_database(&path, ip.is_ipv6())
        .expect("a readable database");

    match geoip.get_location(ip) {
        Ok(location) => println!(
            "{ip}: country={:?} name={:?} continent={:?} coords=({}, {}, accuracy {}) asn={} as_org={:?} anycast={}",
            location.country.code,
            location.country.name,
            location.country.continent.code,
            location.coordinates.latitude,
            location.coordinates.longitude,
            location.coordinates.accuracy_radius,
            location.asn,
            location.as_org,
            location.is_anycast,
        ),
        Err(error) => {
            eprintln!("lookup failed: {error}");
            std::process::exit(1);
        }
    }
}
