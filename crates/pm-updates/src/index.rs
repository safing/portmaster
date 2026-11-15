//! Update indexes and artifacts, a port of Go `service/updates/index.go`.

use std::path::Path;

use serde::{Deserialize, Serialize};
use sha2::{Digest, Sha256};

use crate::version::Version;
use crate::{Error, Result};

/// Maximum size that is allowed to be unpacked.
pub const MAX_UNPACK_SIZE: u64 = 1 << 30;

/// Verification callback for index files.
///
/// Go verifies signatures with `jess`; the caller supplies the verification
/// until the signature layer is ported.
pub type IndexVerifier<'a> = &'a dyn Fn(&[u8]) -> std::result::Result<(), String>;

/// Returns the platform string used by the Go updater (`GOOS_GOARCH`).
pub fn go_platform() -> String {
    let os = std::env::consts::OS;
    let arch = match std::env::consts::ARCH {
        "x86_64" => "amd64",
        "aarch64" => "arm64",
        "x86" => "386",
        "arm" => "arm",
        "powerpc64" => "ppc64",
        other => other,
    };
    format!("{os}_{arch}")
}

/// A single file with metadata.
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
pub struct Artifact {
    #[serde(rename = "Filename")]
    pub filename: String,
    #[serde(rename = "SHA256")]
    pub sha256: String,
    #[serde(
        rename = "URLs",
        default,
        deserialize_with = "crate::deserialize_null_default"
    )]
    pub urls: Vec<String>,
    #[serde(rename = "Platform", default, skip_serializing_if = "String::is_empty")]
    pub platform: String,
    #[serde(rename = "Unpack", default, skip_serializing_if = "String::is_empty")]
    pub unpack: String,
    #[serde(rename = "Version", default, skip_serializing_if = "String::is_empty")]
    pub version: String,
    /// Absolute path of the local file; not serialized.
    #[serde(skip)]
    pub local_file: String,
    /// Parsed version; not serialized.
    #[serde(skip)]
    pub version_num: Option<Version>,
}

impl Artifact {
    /// Returns the required filesystem permission for the artifact.
    pub fn get_file_mode(&self) -> u32 {
        if self.platform == go_platform() {
            0o755
        } else {
            0o644
        }
    }

    /// Returns the absolute path to the local file.
    pub fn path(&self) -> &str {
        &self.local_file
    }

    /// Returns the version of the artifact.
    pub fn semver(&self) -> Option<&Version> {
        self.version_num.as_ref()
    }

    /// Returns whether the artifact is newer than the given one.
    ///
    /// The second value is `false` when the versions cannot be compared.
    pub fn is_newer_than(&self, other: &Artifact) -> (bool, bool) {
        match (&self.version_num, &other.version_num) {
            (_, None) => (true, true),
            (None, _) => (false, false),
            (Some(new), Some(old)) => (new > old, true),
        }
    }

    /// Returns a copy of the artifact with a local path, like Go `export`.
    pub fn export(&self, dir: &str, index_version: Option<&Version>) -> Artifact {
        let mut copied = Artifact {
            filename: self.filename.clone(),
            sha256: self.sha256.clone(),
            urls: self.urls.clone(),
            platform: self.platform.clone(),
            unpack: self.unpack.clone(),
            version: self.version.clone(),
            local_file: Path::new(dir)
                .join(&self.filename)
                .to_string_lossy()
                .into_owned(),
            version_num: self.version_num.clone(),
        };

        if copied.version_num.is_none() {
            if copied.version.is_empty() {
                copied.version_num = index_version.cloned();
            } else if let Ok(version) = Version::parse(&copied.version) {
                copied.version_num = Some(version);
            }
        }

        copied
    }
}

impl std::fmt::Display for Artifact {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "{}(v{})", self.filename, self.version)
    }
}

/// A collection of artifacts with metadata.
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
pub struct Index {
    #[serde(rename = "Name")]
    pub name: String,
    #[serde(rename = "Version")]
    pub version: String,
    #[serde(
        rename = "Published",
        default,
        serialize_with = "serialize_time",
        deserialize_with = "deserialize_time"
    )]
    pub published: i64,
    #[serde(rename = "Artifacts", default)]
    pub artifacts: Vec<Artifact>,
    /// Parsed version; not serialized.
    #[serde(skip)]
    pub version_num: Option<Version>,
    /// Whether the index was generated from a local directory; not serialized.
    #[serde(skip)]
    pub is_locally_generated: bool,
}

fn serialize_time<S: serde::Serializer>(
    seconds: &i64,
    serializer: S,
) -> std::result::Result<S::Ok, S::Error> {
    serializer.serialize_str(&pm_dsd::gotime::format_rfc3339(*seconds))
}

fn deserialize_time<'de, D: serde::Deserializer<'de>>(
    deserializer: D,
) -> std::result::Result<i64, D::Error> {
    let text = String::deserialize(deserializer)?;
    Ok(pm_dsd::gotime::parse_rfc3339(&text).unwrap_or(0))
}

impl Index {
    /// Loads and parses an index from a file.
    pub fn load(
        filename: impl AsRef<Path>,
        platform: &str,
        verifier: Option<IndexVerifier<'_>>,
    ) -> Result<Index> {
        let content = std::fs::read(filename).map_err(Error::ReadIndex)?;
        Index::parse(&content, platform, verifier)
    }

    /// Parses an index from JSON.
    pub fn parse(
        content: &[u8],
        platform: &str,
        verifier: Option<IndexVerifier<'_>>,
    ) -> Result<Index> {
        if let Some(verifier) = verifier {
            verifier(content).map_err(Error::Verify)?;
        }

        let mut index: Index =
            serde_json::from_slice(content).map_err(|error| Error::Parse(error.to_string()))?;
        let platform = if platform.is_empty() {
            go_platform()
        } else {
            platform.to_string()
        };
        index.init(&platform)?;
        Ok(index)
    }

    /// Initializes the index: parses versions, filters artifacts by platform
    /// and validates artifact filenames.
    fn init(&mut self, platform: &str) -> Result<()> {
        if !self.version.is_empty() {
            let version = Version::parse(&self.version)
                .map_err(|error| Error::InvalidVersion(self.version.clone(), error.to_string()))?;
            self.version_num = Some(version);
        }

        let mut filtered = Vec::with_capacity(self.artifacts.len());
        for artifact in self.artifacts.drain(..) {
            if artifact.platform.is_empty() || artifact.platform == platform {
                filtered.push(artifact);
            }
        }
        self.artifacts = filtered;

        for artifact in &self.artifacts {
            validate_artifact_filename(&artifact.filename)?;
        }

        for artifact in &mut self.artifacts {
            if artifact.version.is_empty() {
                artifact.version = self.version.clone();
                artifact.version_num = self.version_num.clone();
            } else if let Ok(version) = Version::parse(&artifact.version) {
                artifact.version_num = Some(version);
            }
        }

        Ok(())
    }

    /// Sets whether the index was generated from a local directory.
    pub fn set_locally_generated(&mut self, locally_generated: bool) {
        self.is_locally_generated = locally_generated;
    }

    /// Returns whether the index is able to follow a secure upgrade path.
    pub fn can_do_upgrades(&self, now: i64) -> Result<()> {
        let Some(version) = &self.version_num else {
            return Err(Error::MissingVersion);
        };
        let _ = version;

        if self.published == 0 {
            return Err(Error::MissingPublishDate);
        }
        if self.published > now + 15 * 60 {
            let difference = self.published - now;
            // Go rounds the remaining time to the minute.
            let rounded = (difference + 30) / 60 * 60;
            return Err(Error::FromTheFuture(format_go_duration(rounded)));
        }

        Ok(())
    }

    /// Returns whether the given index is a successor and should be upgraded to.
    pub fn should_upgrade_to(&self, new_index: &Index, now: i64) -> Result<()> {
        self.can_do_upgrades(now)
            .map_err(|error| Error::CurrentCannotUpgrade(error.to_string()))?;
        new_index
            .can_do_upgrades(now)
            .map_err(|error| Error::NewCannotUpgrade(error.to_string()))?;

        let current = self
            .version_num
            .as_ref()
            .expect("checked by can_do_upgrades");
        let new = new_index
            .version_num
            .as_ref()
            .expect("checked by can_do_upgrades");

        if current.is_zero() {
            // The zero version is used for bootstrapping; upgrade in any case.
            return Ok(());
        }

        if self.name != new_index.name {
            return Err(Error::NameMismatch {
                current: self.name.clone(),
                new: new_index.name.clone(),
            });
        }

        if current < new && self.published > new_index.published && !self.is_locally_generated {
            return Err(Error::NewerVersionOlderDate);
        }
        if current > new && self.published < new_index.published && !self.is_locally_generated {
            return Err(Error::OlderVersionNewerDate);
        }
        if current.segments()[0] > new.segments()[0] {
            // Downgrades are only allowed when they are not breaking changes.
            return Err(Error::BreakingDowngrade);
        }
        if current > new && self.published > now - 20 * 60 {
            // The current index was just updated; an older one is likely CDN
            // caching.
            return Err(Error::SameIndex);
        }
        if self.published == new_index.published {
            return Err(Error::SameIndex);
        }
        if current == new && self.is_locally_generated {
            return Err(Error::SameIndex);
        }

        Ok(())
    }

    /// Checks that all artifacts are present in the given directory and have
    /// the correct hash.
    pub fn verify_artifacts(&self, dir: impl AsRef<Path>) -> Result<()> {
        for artifact in &self.artifacts {
            let path = dir.as_ref().join(&artifact.filename);
            check_sha256_sum_file(&path, &artifact.sha256).map_err(|error| {
                Error::ArtifactVerification(artifact.filename.clone(), error.to_string())
            })?;
        }
        Ok(())
    }

    /// Serializes the index to JSON, like Go `Export` without a signing key.
    pub fn to_json(&self) -> Result<Vec<u8>> {
        Ok(serde_json::to_vec(self)?)
    }
}

/// Validates an artifact filename, like Go `validateArtifactFilename`.
pub fn validate_artifact_filename(name: &str) -> Result<()> {
    if name.is_empty() || name == "." {
        return Err(Error::InvalidFilename(name.to_string()));
    }

    // Normalize backslashes for cross-platform consistency.
    let normalized = name.replace('\\', "/");

    if clean_path(&normalized) != normalized {
        return Err(Error::NonCanonicalFilename(name.to_string()));
    }
    if !valid_path(&normalized) {
        return Err(Error::InvalidPathName(name.to_string()));
    }
    if normalized.contains('/') {
        return Err(Error::FilenameComponents(name.to_string()));
    }
    if normalized.contains(':') {
        return Err(Error::FilenameColon(name.to_string()));
    }

    Ok(())
}

/// Cleans a slash-separated path, like Go `path.Clean`.
fn clean_path(path: &str) -> String {
    let rooted = path.starts_with('/');
    let mut parts: Vec<&str> = Vec::new();

    for part in path.split('/') {
        match part {
            "" | "." => {}
            ".." => match parts.last() {
                Some(last) if *last != ".." => {
                    parts.pop();
                }
                _ if !rooted => parts.push(".."),
                _ => {}
            },
            other => parts.push(other),
        }
    }

    let joined = parts.join("/");
    if rooted {
        format!("/{joined}")
    } else if joined.is_empty() {
        ".".to_string()
    } else {
        joined
    }
}

/// Returns whether the path is unrooted and has no `.` or `..` elements, like
/// Go `fs.ValidPath`.
fn valid_path(path: &str) -> bool {
    if path.is_empty() || path == "." || path.starts_with('/') {
        return false;
    }
    path.split('/')
        .all(|part| !part.is_empty() && part != "." && part != "..")
}

/// Checks the SHA-256 sum of a file.
pub fn check_sha256_sum_file(filename: impl AsRef<Path>, sha256sum: &str) -> Result<()> {
    let data = std::fs::read(filename).map_err(Error::OpenFile)?;
    check_sha256_sum(&data, sha256sum)
}

/// Checks the SHA-256 sum of the data.
pub fn check_sha256_sum(data: &[u8], sha256sum: &str) -> Result<()> {
    let expected =
        hex::decode(sha256sum).map_err(|_| Error::InvalidHashHex(sha256sum.to_string()))?;
    if expected.len() != 32 {
        return Err(Error::InvalidHashSize(sha256sum.to_string()));
    }

    let digest = Sha256::digest(data);
    if digest.as_slice() != expected.as_slice() {
        return Err(Error::ChecksumMismatch);
    }
    Ok(())
}

/// Formats a duration in seconds like Go's `time.Duration.String`.
fn format_go_duration(seconds: i64) -> String {
    let hours = seconds / 3600;
    let minutes = (seconds % 3600) / 60;
    let seconds = seconds % 60;
    if hours > 0 {
        format!("{hours}h{minutes}m{seconds}s")
    } else if minutes > 0 {
        format!("{minutes}m{seconds}s")
    } else {
        format!("{seconds}s")
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn filenames_are_validated_like_go() {
        for name in [
            "filterlists.zip",
            "geoipv4.mmdb",
            "base.dsdl",
            "file.with.dots.tar.gz",
        ] {
            assert!(validate_artifact_filename(name).is_ok(), "{name}");
        }
        for name in ["", ".", "..", "../evil", "./evil", "dir/file", "C:file"] {
            assert!(validate_artifact_filename(name).is_err(), "{name}");
        }
    }

    #[test]
    fn paths_are_cleaned_like_go() {
        assert_eq!(clean_path("a/../evil"), "evil");
        assert_eq!(clean_path("./evil"), "evil");
        assert_eq!(clean_path("//evil"), "/evil");
        assert_eq!(clean_path("evil/"), "evil");
        assert_eq!(clean_path(".."), "..");
        assert_eq!(clean_path("."), ".");
        assert_eq!(clean_path(""), ".");
    }

    #[test]
    fn platform_matches_go_naming() {
        let platform = go_platform();
        assert!(platform.starts_with(std::env::consts::OS));
        assert!(!platform.contains("x86_64"), "{platform}");
    }
}
