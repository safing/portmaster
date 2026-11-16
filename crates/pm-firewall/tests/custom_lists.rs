//! Tests for the custom filter list firewall adapter.

use std::io::Write;
use std::sync::Arc;

use pm_firewall::{CustomLists, ServiceCustomLists};
use pm_intel::customlists::CustomListService;

fn service_with(content: &str) -> ServiceCustomLists {
    let path = std::env::temp_dir().join(format!(
        "pm-firewall-customlists-{}-{}.txt",
        std::process::id(),
        std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)
            .unwrap()
            .subsec_nanos()
    ));
    let mut file = std::fs::File::create(&path).expect("create list");
    file.write_all(content.as_bytes()).expect("write list");
    drop(file);

    let service = Arc::new(CustomListService::new());
    service
        .check_and_update(path.to_str().unwrap())
        .expect("load list");
    let _ = std::fs::remove_file(&path);
    ServiceCustomLists::new(service)
}

#[test]
fn exposes_the_loaded_lists() {
    let lists = service_with("example.com\n1.2.3.4\nUS\nAS1234\n");

    assert!(lists.is_loaded());
    assert_eq!(
        lists.lookup_domain("example.com.", false),
        (true, "example.com.".to_string())
    );
    assert_eq!(
        lists.lookup_domain("sub.example.com.", true),
        (true, "example.com.".to_string())
    );
    assert!(lists.lookup_ip("1.2.3.4".parse().unwrap()));
    assert!(!lists.lookup_ip("1.2.3.5".parse().unwrap()));
    assert!(lists.lookup_asn(1234));
    assert!(lists.lookup_country("US"));
    assert!(!lists.lookup_country("DE"));
}

#[test]
fn empty_service_is_not_loaded() {
    let lists = ServiceCustomLists::new(Arc::new(CustomListService::new()));
    assert!(!lists.is_loaded());
    assert!(!lists.lookup_ip("1.2.3.4".parse().unwrap()));
    assert_eq!(lists.lookup_domain("example.com.", true), (false, String::new()));
}
