//! Conversion of legacy v1/v2 update indexes to the v3 format, port of Go
//! `cmds/updatemgr/convert.go`.

use std::collections::BTreeMap;
use std::time::SystemTime;

use pm_dsd::gotime;
use pm_updates::{Artifact, Index, IndexScanConfig, PatternMatcher};

/// Ignore patterns copied verbatim from Go `cmds/updatemgr/scan.go`.
pub const IGNORE_FILES: &[&str] = &[
    "*.json",
    "sha256*.txt",
    "latest/**",
    "*.sig",
    "**/*.sig",
    "**/*.apk",
    "**/*install*",
    "**/spn-hub*",
    "**/jess*",
    "**/hubs*.json",
    "**/*mini*.mmdb.gz",
    "darwin_amd64/**",
    "darwin_arm64/**",
    "**/portmaster-start*",
    "**/portmaster-app*",
    "**/portmaster-notifier*",
    "**/portmaster-wintoast*.dll",
    "**/portmaster-snoretoast*.exe",
    "**/portmaster-kext*.dll",
    "**/profilemgr*.zip",
    "**/settings*.zip",
    "**/monitor*.zip",
    "**/base*.zip",
    "**/console*.zip",
];

/// Old v2 index format.
#[derive(Debug, serde::Deserialize)]
pub struct IndexV2 {
    /// Release channel.
    #[serde(rename = "Channel", alias = "channel", default)]
    pub _channel: String,
    /// Publication time, like Go `time.Time` in JSON.
    #[serde(rename = "Published", alias = "published", deserialize_with = "deserialize_published")]
    pub published: i64,
    /// Identifier → version map.
    #[serde(rename = "Releases", alias = "releases", default)]
    pub releases: BTreeMap<String, String>,
}

fn deserialize_published<'de, D>(deserializer: D) -> Result<i64, D::Error>
where
    D: serde::Deserializer<'de>,
{
    use serde::Deserialize;
    let value = serde_json::Value::deserialize(deserializer)?;
    let seconds = match value {
        serde_json::Value::Number(number) => number.as_i64().unwrap_or(0),
        serde_json::Value::String(text) => gotime::parse_rfc3339(&text).unwrap_or(0),
        _ => 0,
    };
    Ok(seconds)
}

/// Combines an identifier and a version into the released file path, like Go
/// `GetVersionedPath`.
pub fn get_versioned_path(identifier: &str, version: &str) -> String {
    let (directory, filename) = match identifier.rsplit_once('/') {
        Some((dir, name)) => (format!("{dir}/"), name),
        None => (String::new(), identifier),
    };
    let mut parts = filename.splitn(2, '.');
    let stem = parts.next().unwrap_or("");
    let extension = parts.next();
    let transformed = version.replacen('.', "-", 2);
    let mut path = format!("{directory}{stem}_v{transformed}");
    if let Some(extension) = extension {
        path.push('.');
        path.push_str(extension);
    }
    path
}

/// Converts the entries of an old index into v3 artifacts.
pub fn convert_entries(
    index: &mut Index,
    base_url: &str,
    entries: &BTreeMap<String, String>,
    patterns: &PatternMatcher,
) -> Result<(), String> {
    for (identifier, version) in entries {
        let artifact_path = get_versioned_path(identifier, version);
        if patterns.is_ignored(&artifact_path) {
            continue;
        }

        let directory = identifier.rsplit_once('/').map_or("", |(dir, _)| dir);
        let filename = identifier.rsplit_once('/').map_or(identifier.as_str(), |(_, name)| name);
        let mut platform = directory.split('/').next().unwrap_or("");
        if platform == "all" {
            platform = "";
        }
        if platform.is_empty() && directory.is_empty() {
            continue;
        }

        // Go passes only the file name here, so `**/*.gz` never matches a
        // converted artifact; use the full released path instead.
        let unpack = patterns
            .unpack_setting(&artifact_path)
            .map_err(|error| format!("failed to get unpack setting for {filename}: {error}"))?;
        index.artifacts.push(Artifact {
            filename: filename.to_string(),
            urls: vec![format!("{base_url}{artifact_path}")],
            platform: platform.to_string(),
            unpack,
            version: version.clone(),
            ..Artifact::default()
        });
    }
    Ok(())
}

/// Converts a v1 index (`identifier → version` map).
pub fn convert_v1(
    index_data: &[u8],
    base_url: &str,
    published: i64,
    config: &IndexScanConfig,
) -> Result<Index, String> {
    let old: BTreeMap<String, String> = serde_json::from_slice(index_data)
        .map_err(|error| format!("failed to parse old v1 index: {error}"))?;
    let patterns = config
        .prepare()
        .map_err(|error| format!("invalid scan config: {error}"))?;

    let mut index = Index {
        published,
        ..Index::default()
    };
    convert_entries(&mut index, base_url, &old, &patterns)?;
    Ok(index)
}

/// Converts a v2 index.
pub fn convert_v2(
    index_data: &[u8],
    base_url: &str,
    config: &IndexScanConfig,
) -> Result<Index, String> {
    let old: IndexV2 = serde_json::from_slice(index_data)
        .map_err(|error| format!("failed to parse old v2 index: {error}"))?;
    let patterns = config
        .prepare()
        .map_err(|error| format!("invalid scan config: {error}"))?;

    let mut index = Index {
        published: old.published,
        ..Index::default()
    };
    convert_entries(&mut index, base_url, &old.releases, &patterns)?;
    Ok(index)
}

/// Publication time in seconds, like Go `time.Now()` used by the caller.
pub fn now() -> i64 {
    SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .map_or(0, |elapsed| elapsed.as_secs() as i64)
}

#[cfg(test)]
mod tests {
    use super::*;

    fn config() -> IndexScanConfig {
        IndexScanConfig {
            ignore_files: vec!["*.json".to_string(), "**/*.sig".to_string()],
            unpack_files: BTreeMap::from([("gz".to_string(), "**/*.gz".to_string())]),
            ..IndexScanConfig::default()
        }
    }

    #[test]
    fn versioned_paths_match_go() {
        assert_eq!(
            get_versioned_path("linux_amd64/portmaster-core", "1.2.3"),
            "linux_amd64/portmaster-core_v1-2-3"
        );
        assert_eq!(
            get_versioned_path("all/geoip.mmdb.gz", "2025.04.14"),
            "all/geoip_v2025-04-14.mmdb.gz"
        );
        assert_eq!(
            get_versioned_path("tool.exe", "2.0.0"),
            "tool_v2-0-0.exe"
        );
    }

    #[test]
    fn converts_a_v1_index() {
        let data = br#"{"linux_amd64/portmaster-core":"1.2.3","index.json":"1.2.3","linux_amd64/foo.sig":"1.2.3"}"#;
        let index = convert_v1(data, "https://updates.example.com/", 1700, &config()).unwrap();
        assert_eq!(index.published, 1700);
        assert_eq!(index.artifacts.len(), 1);
        let artifact = &index.artifacts[0];
        assert_eq!(artifact.filename, "portmaster-core");
        assert_eq!(artifact.platform, "linux_amd64");
        assert_eq!(artifact.version, "1.2.3");
        assert_eq!(
            artifact.urls,
            vec!["https://updates.example.com/linux_amd64/portmaster-core_v1-2-3"]
        );
    }

    #[test]
    fn converts_a_v2_index_with_unpack() {
        let data = br#"{"Channel":"stable","Published":"1970-01-01T00:28:20Z","Releases":{"linux_amd64/intel.mmdb.gz":"2025.4.14"}}"#;
        let index = convert_v2(data, "https://updates.example.com/", &config()).unwrap();
        assert_eq!(index.published, 1700);
        assert_eq!(index.artifacts.len(), 1);
        assert_eq!(index.artifacts[0].unpack, "gz");
        assert_eq!(index.artifacts[0].version, "2025.4.14");
    }
}
