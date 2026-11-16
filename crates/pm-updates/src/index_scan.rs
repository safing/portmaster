//! Port of Go `service/updates/index_scan.go`: generate an update index from a
//! directory tree.

use std::collections::BTreeMap;
use std::path::{Path, PathBuf};
use std::sync::LazyLock;
use std::time::{SystemTime, UNIX_EPOCH};

use regex::Regex;
use sha2::{Digest, Sha256};

use crate::{decompress, Artifact, Glob, Index, Version};

/// Matches `_v1-2-3` and `_v1-2-3-beta` in file names, like Go.
static FILE_VERSION_REGEX: LazyLock<Regex> = LazyLock::new(|| {
    Regex::new(r"_v[0-9]+-[0-9]+-[0-9]+(-[a-z]+)?").expect("valid regex")
});

/// Errors of the index scan.
#[derive(Debug, thiserror::Error)]
pub enum ScanError {
    #[error("invalid ignore files pattern {0:?}: {1}")]
    IgnorePattern(String, String),
    #[error("invalid unpack files pattern {0:?}: {1}")]
    UnpackPattern(String, String),
    #[error("invalid index dir: {0}")]
    IndexDir(#[source] std::io::Error),
    #[error("invalid index version: {0}")]
    IndexVersion(String),
    #[error("invalid version {0} for {1}: {2}")]
    Version(String, String, String),
    #[error("duplicate version for {0}: {1} and {2}")]
    DuplicateVersion(String, String, String),
    #[error("both a versioned and non-versioned file for: {0}: {1} and {2}")]
    MixedVersion(String, String, String),
    #[error("matches contradicting unpack settings")]
    ContradictingUnpack,
    #[error("invalid unpack setting for {0} at {1}: {2}")]
    UnpackSetting(String, String, String),
    #[error("scanning dir: {0}")]
    Walk(#[source] std::io::Error),
    #[error("calculate hash of file: {0} {1}")]
    Hash(String, String),
}

/// Configuration of an index scan, like Go `IndexScanConfig`.
#[derive(Debug, Clone, Default)]
pub struct IndexScanConfig {
    /// Name of the index; defaults to the directory name.
    pub name: String,
    /// Version of the index; taken from the primary artifact if empty.
    pub version: String,
    /// Key of the artifact whose version becomes the index version.
    pub primary_artifact: String,
    /// Base URL that is prepended to the relative path of every artifact.
    pub base_url: String,
    /// Per-identifier artifact templates.
    pub templates: BTreeMap<String, Artifact>,
    /// Glob patterns of files that are not part of the index.
    pub ignore_files: Vec<String>,
    /// Unpack setting per glob pattern.
    pub unpack_files: BTreeMap<String, String>,
}

impl IndexScanConfig {
    /// Compiles the glob patterns and normalizes the base URL.
    /// Compiles the ignore and unpack patterns for reuse by other tools,
    /// like Go `IndexScanConfig.init`.
    pub fn prepare(&self) -> Result<PatternMatcher, ScanError> {
        let base_url = format!("{}/", self.base_url.trim_end_matches('/'));
        let cleaned_base_url = if self.base_url.is_empty() {
            String::new()
        } else {
            base_url
        };

        let mut ignore_globs = Vec::with_capacity(self.ignore_files.len());
        for pattern in &self.ignore_files {
            let glob = Glob::compile(pattern).map_err(|error| {
                ScanError::IgnorePattern(pattern.clone(), error.to_string())
            })?;
            ignore_globs.push(glob);
        }

        let mut unpack_globs = Vec::with_capacity(self.unpack_files.len());
        for (setting, pattern) in &self.unpack_files {
            let glob = Glob::compile(pattern)
                .map_err(|error| ScanError::UnpackPattern(pattern.clone(), error.to_string()))?;
            unpack_globs.push((setting.clone(), glob));
        }

        Ok(PatternMatcher {
            cleaned_base_url,
            ignore_globs,
            unpack_globs,
        })
    }
}

/// A compiled index scan configuration.
/// Compiled ignore/unpack patterns of an [`IndexScanConfig`].
pub struct PatternMatcher {
    cleaned_base_url: String,
    ignore_globs: Vec<Glob>,
    unpack_globs: Vec<(String, Glob)>,
}

impl PatternMatcher {
    /// Returns whether a file name should be ignored.
    pub fn is_ignored(&self, filename: &str) -> bool {
        self.ignore_globs
            .iter()
            .any(|pattern| pattern.is_match(filename))
    }

    /// Returns the unpack setting for the file name.
    pub fn unpack_setting(&self, filename: &str) -> Result<String, ScanError> {
        let mut found: Option<&str> = None;
        for (setting, pattern) in &self.unpack_globs {
            if !pattern.is_match(filename) {
                continue;
            }
            match found {
                None => found = Some(setting),
                Some(existing) if existing != setting => {
                    return Err(ScanError::ContradictingUnpack);
                }
                Some(_) => {}
            }
        }
        Ok(found.unwrap_or_default().to_string())
    }
}

/// Generates an index from the given directory.
pub fn generate_index_from_dir(
    source_dir: impl AsRef<Path>,
    config: &IndexScanConfig,
) -> Result<Index, ScanError> {
    let prepared = config.prepare()?;

    let source_dir = std::path::absolute(source_dir.as_ref()).map_err(ScanError::IndexDir)?;
    let index_version = if config.version.is_empty() {
        None
    } else {
        Some(
            Version::parse(&config.version)
                .map_err(|error| ScanError::IndexVersion(error.to_string()))?,
        )
    };

    let mut files = Vec::new();
    walk(&source_dir, &source_dir, &mut files)?;

    let mut artifacts: BTreeMap<String, Artifact> = BTreeMap::new();

    for (relpath, fullpath) in files {
        // Skip files that are explicitly ignored.
        if prepared.is_ignored(&relpath) {
            continue;
        }

        let filename = fullpath
            .file_name()
            .map(|name| name.to_string_lossy().into_owned())
            .unwrap_or_default();

        // Extract the version, if present; otherwise the file name is the
        // identifier, which is normal for the simplified system.
        let (identifier, version) = match get_identifier_and_version(&filename) {
            Some((identifier, version)) => (identifier, version),
            None => (filename, String::new()),
        };
        let version_num = if version.is_empty() {
            None
        } else {
            Some(Version::parse(&version).map_err(|error| {
                ScanError::Version(relpath.clone(), version.clone(), error.to_string())
            })?)
        };

        // The first path component is the platform; top-level files are for all.
        let platform = relpath
            .split_once('/')
            .map_or_else(|| "all".to_string(), |(platform, _)| platform.to_string());
        let key = format!("{platform}/{identifier}");

        if let Some(existing) = artifacts.get(&key) {
            if existing.version == version {
                return Err(ScanError::DuplicateVersion(
                    key,
                    existing.local_file.clone(),
                    fullpath.to_string_lossy().into_owned(),
                ));
            }
            if existing.version.is_empty() != version.is_empty() {
                return Err(ScanError::MixedVersion(
                    key,
                    existing.local_file.clone(),
                    fullpath.to_string_lossy().into_owned(),
                ));
            }

            let existing_version = Version::parse(&existing.version).ok();
            if existing_version == version_num {
                return Err(ScanError::DuplicateVersion(
                    key,
                    existing.local_file.clone(),
                    fullpath.to_string_lossy().into_owned(),
                ));
            }
            // The new version is older: keep the existing artifact.
            if existing_version
                .as_ref()
                .is_some_and(|existing| version_num.as_ref().is_some_and(|new| existing > new))
            {
                continue;
            }
        }

        let mut artifact = config
            .templates
            .get(&identifier)
            .cloned()
            .unwrap_or_default();

        if artifact.filename.is_empty() {
            artifact.filename = identifier;
        }
        if artifact.urls.is_empty() && !config.base_url.is_empty() {
            artifact.urls = vec![format!("{}{}", prepared.cleaned_base_url, relpath)];
        }
        if artifact.platform.is_empty() {
            artifact.platform = platform;
        }
        if artifact.unpack.is_empty() {
            artifact.unpack = prepared.unpack_setting(&relpath).map_err(|error| {
                ScanError::UnpackSetting(key.clone(), relpath.clone(), error.to_string())
            })?;
        }
        if artifact.version.is_empty() {
            artifact.version = version;
        }

        // Remove the unpack suffix from the file name.
        if !artifact.unpack.is_empty() {
            let suffix = format!(".{}", artifact.unpack);
            if let Some(stripped) = artifact.filename.strip_suffix(&suffix) {
                artifact.filename = stripped.to_string();
            }
        }

        artifact.local_file = fullpath.to_string_lossy().into_owned();
        artifacts.insert(key, artifact);
    }

    let mut index = Index {
        name: config.name.clone(),
        version: config.version.clone(),
        published: now_seconds(),
        artifacts: Vec::new(),
        version_num: index_version,
        is_locally_generated: true,
    };

    if index.version.is_empty() && !config.primary_artifact.is_empty() {
        if let Some(primary) = artifacts.get(&config.primary_artifact) {
            index.version = primary.version.clone();
        }
    }
    if index.name.is_empty() {
        index.name = source_dir
            .file_name()
            .map(|name| name.to_string_lossy().into_owned())
            .unwrap_or_default()
            .trim_matches(|char| char == '.' || char == '/' || char == '\\')
            .to_string();
    }

    let mut export = Vec::with_capacity(artifacts.len());
    for mut artifact in artifacts.into_values() {
        artifact.sha256 = sha256_of_file(&artifact.local_file, &artifact.unpack)
            .map_err(|error| ScanError::Hash(artifact.local_file.clone(), error))?;

        // Drop platform and version values that the index already provides.
        if artifact.platform == "all" {
            artifact.platform.clear();
        }
        if artifact.version == index.version {
            artifact.version.clear();
        }

        export.push(artifact);
    }

    export.sort_by(|a, b| {
        a.filename
            .cmp(&b.filename)
            .then_with(|| a.platform.cmp(&b.platform))
            .then_with(|| a.version.cmp(&b.version))
            .then_with(|| a.sha256.cmp(&b.sha256))
    });

    index.artifacts = export;
    Ok(index)
}

/// Collects all files below `dir` in the order of Go's `filepath.WalkDir`
/// (lexically sorted per directory).
fn walk(
    root: &Path,
    dir: &Path,
    files: &mut Vec<(String, PathBuf)>,
) -> Result<(), ScanError> {
    let mut entries = Vec::new();
    for entry in std::fs::read_dir(dir).map_err(ScanError::Walk)? {
        let entry = entry.map_err(ScanError::Walk)?;
        entries.push(entry);
    }
    entries.sort_by_key(|entry| entry.file_name());

    for entry in entries {
        let path = entry.path();
        // `DirEntry::file_type` does not follow symlinks, like `WalkDir`.
        let file_type = entry.file_type().map_err(ScanError::Walk)?;

        if file_type.is_dir() {
            walk(root, &path, files)?;
            continue;
        }

        let relpath = path
            .strip_prefix(root)
            .unwrap_or(&path)
            .to_string_lossy()
            .replace(std::path::MAIN_SEPARATOR, "/");
        files.push((relpath, path));
    }

    Ok(())
}

/// Returns the SHA-256 sum of the file, unpacking it if necessary.
pub fn sha256_of_file(path: &str, unpack_type: &str) -> Result<String, String> {
    let mut content = std::fs::read(path).map_err(|error| error.to_string())?;

    if !unpack_type.is_empty() {
        content = decompress(unpack_type, &content).map_err(|error| error.to_string())?;
    }

    let mut hasher = Sha256::new();
    hasher.update(&content);
    Ok(hex::encode(hasher.finalize()))
}

/// Splits a file name into the identifier and the embedded version.
///
/// Returns `None` when the name has no `_v<major>-<minor>-<patch>` part.
fn get_identifier_and_version(filename: &str) -> Option<(String, String)> {
    let raw_version = FILE_VERSION_REGEX.find(filename)?.as_str();

    // Trim the `_v` that the regex catches and turn the first two dashes into
    // dots to get the version string.
    let version = raw_version
        .trim_start_matches(&['_', 'v'][..])
        .replacen('-', ".", 2);

    let start = filename.find(raw_version)?;
    let identifier = format!(
        "{}{}",
        &filename[..start],
        &filename[start + raw_version.len()..]
    );

    Some((identifier, version))
}

/// Returns the current time in seconds since the Unix epoch.
fn now_seconds() -> i64 {
    match SystemTime::now().duration_since(UNIX_EPOCH) {
        Ok(duration) => duration.as_secs() as i64,
        Err(_) => 0,
    }
}
