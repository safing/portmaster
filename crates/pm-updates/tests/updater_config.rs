//! Tests for the updater configuration (Go `updates.Config.Check`).

use pm_updates::UpdaterConfig;

fn valid_config() -> UpdaterConfig {
    UpdaterConfig {
        name: "test".to_string(),
        directory: "/bin".to_string(),
        download_directory: "/download".to_string(),
        purge_directory: "/purge".to_string(),
        index_file: "index.json".to_string(),
        index_urls: vec!["https://updates.example.com/index.json".to_string()],
        ..UpdaterConfig::default()
    }
}

#[test]
fn valid_config_passes_and_fills_the_platform() {
    let mut config = valid_config();
    config.check().expect("valid config");
    assert_eq!(config.platform, pm_updates::go_platform());
}

#[test]
fn missing_fields_use_the_go_messages() {
    let cases: &[(&str, &str)] = &[
        ("name", "name must be set"),
        ("directory", "directory must be set"),
        ("download_directory", "download directory must be set"),
        ("purge_directory", "purge directory must be set"),
        ("index_file", "index file must be set"),
    ];

    for (field, message) in cases {
        let mut config = valid_config();
        match *field {
            "name" => config.name.clear(),
            "directory" => config.directory.clear(),
            "download_directory" => config.download_directory.clear(),
            "purge_directory" => config.purge_directory.clear(),
            "index_file" => config.index_file.clear(),
            _ => unreachable!(),
        }
        assert_eq!(config.check().unwrap_err().0, *message);
    }
}

#[test]
fn auto_apply_requires_auto_download() {
    let mut config = valid_config();
    config.auto_apply = true;
    assert_eq!(
        config.check().unwrap_err().0,
        "auto apply is set, but auto download is not"
    );

    config.auto_download = true;
    config.check().expect("auto download is set");
}

#[test]
fn ignore_entries_must_be_names() {
    let mut config = valid_config();
    config.ignore = vec!["ok".to_string(), "bin/tool".to_string()];
    assert_eq!(
        config.check().unwrap_err().0,
        "ignore entry #2 invalid: must be file or directory name, not path"
    );

    // On Linux `filepath.Separator` is only `/`, so a backslash is a valid name.
    config.ignore = vec!["ok".to_string(), "sub\\tool".to_string()];
    config.check().expect("backslash is not a separator on Linux");
}

#[test]
fn index_urls_must_use_https() {
    let mut config = valid_config();
    config.index_urls = vec![
        "https://updates.example.com/index.json".to_string(),
        "http://updates.example.com/index.json".to_string(),
    ];
    assert_eq!(
        config.check().unwrap_err().0,
        "index URL #2 invalid: is not a HTTPS url"
    );
}

#[test]
fn index_path_joins_the_directory() {
    let config = valid_config();
    assert_eq!(
        config.index_path("/bin"),
        std::path::Path::new("/bin/index.json")
    );
}
