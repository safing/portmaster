//! Prints the generated Xray JSON for one share link (one line per link).
//! Handy when cross-checking the generator against `xray-rust config check`.

use pm_xray::config::{generate_config, InboundProtocol};
use pm_xray::{Protocol, XrayNode};

fn main() {
    let links: Vec<String> = std::env::args().skip(1).collect();
    if links.is_empty() {
        eprintln!("usage: dump_config <share-link>...");
        std::process::exit(2);
    }
    for raw in links {
        let node = XrayNode {
            id: "dump".into(),
            subscription_id: "dump".into(),
            name: String::new(),
            country_code: String::new(),
            country_name: String::new(),
            latitude: None,
            longitude: None,
            protocol: Protocol::Unknown,
            address: String::new(),
            port: 0,
            raw_config: raw.clone(),
        };
        match generate_config(&node, 1080, InboundProtocol::Socks) {
            Ok(config) => println!("{}", serde_json::to_string(&config).unwrap()),
            Err(error) => {
                eprintln!("{raw}: {error}");
                std::process::exit(1);
            }
        }
    }
}
