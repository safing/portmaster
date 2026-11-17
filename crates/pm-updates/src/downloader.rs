//! Downloading and verifying update indexes and artifacts, a port of Go
//! `service/updates/downloader.go`.

use std::collections::HashMap;
use std::io::Read;
use std::path::{Path, PathBuf};

use sha2::{Digest, Sha256};

use crate::index::{check_sha256_sum, Index, IndexVerifier};
use crate::{Error, Result};

/// HTTP client used to fetch indexes and artifacts.
pub trait HttpClient: Send + Sync {
    /// Downloads the given URL and returns the body.
    ///
    /// Implementations report transport and HTTP errors as text, like Go's
    /// `downloadData` does.
    fn get(&self, url: &str) -> std::result::Result<Vec<u8>, String>;
}

/// A blocking HTTPS client based on `ureq`.
pub struct HttpsClient {
    agent: ureq::Agent,
    user_agent: String,
}

impl HttpsClient {
    /// Creates a client with the Go updater's user agent.
    pub fn new() -> HttpsClient {
        HttpsClient {
            agent: ureq::Agent::new_with_defaults(),
            user_agent: default_user_agent(),
        }
    }

    /// Overrides the user agent.
    pub fn with_user_agent(mut self, user_agent: &str) -> HttpsClient {
        self.user_agent = user_agent.to_string();
        self
    }
}

impl Default for HttpsClient {
    fn default() -> Self {
        HttpsClient::new()
    }
}

/// Returns the user agent of the Go updater: `Portmaster (<os> <arch>)`.
pub fn default_user_agent() -> String {
    format!(
        "Portmaster ({} {})",
        std::env::consts::OS,
        std::env::consts::ARCH
    )
}

impl HttpClient for HttpsClient {
    fn get(&self, url: &str) -> std::result::Result<Vec<u8>, String> {
        let mut response = self
            .agent
            .get(url)
            .header("User-Agent", &self.user_agent)
            .call()
            .map_err(|error| match error {
                ureq::Error::StatusCode(code) => {
                    format!("server returned non-OK status: {code}")
                }
                other => format!("failed a get file request to: {other}"),
            })?;

        let mut data = Vec::new();
        response
            .body_mut()
            .as_reader()
            .read_to_end(&mut data)
            .map_err(|error| format!("failed to read body of response: {error}"))?;
        Ok(data)
    }
}

/// Decompresses data according to the unpack type, like Go `Decompress`.
pub fn decompress(content_type: &str, data: &[u8]) -> Result<Vec<u8>> {
    match content_type {
        "zip" => decompress_zip(data),
        "gz" => decompress_gzip(data),
        _ => Err(Error::Process("unsupported compression type".to_string())),
    }
}

fn decompress_gzip(data: &[u8]) -> Result<Vec<u8>> {
    // Go's gzip.Reader transparently reads concatenated members; the single
    // member GzDecoder would silently stop after the first one.
    let reader = flate2::read::MultiGzDecoder::new(data);
    read_limited(reader, "read gzip file")
}

fn decompress_zip(data: &[u8]) -> Result<Vec<u8>> {
    let mut archive = zip::ZipArchive::new(std::io::Cursor::new(data))
        .map_err(|error| Error::Process(format!("create zip reader: {error}")))?;
    if archive.len() != 1 {
        return Err(Error::Process(
            "zip file must contain exactly one file".to_string(),
        ));
    }

    let mut file = archive
        .by_index(0)
        .map_err(|error| Error::Process(format!("open file in zip: {error}")))?;
    read_limited(&mut file, "read file in zip")
}

/// Reads at most `MAX_UNPACK_SIZE` bytes from the reader, like Go's `CopyN`.
fn read_limited(mut reader: impl Read, context: &str) -> Result<Vec<u8>> {
    let mut data = Vec::new();
    reader
        .by_ref()
        .take(crate::MAX_UNPACK_SIZE)
        .read_to_end(&mut data)
        .map_err(|error| Error::Process(format!("{context}: {error}")))?;
    Ok(data)
}

/// Downloads indexes and artifacts, like Go `Downloader`.
pub struct Downloader {
    index_urls: Vec<String>,
    existing_files: HashMap<String, String>,
}

impl Downloader {
    /// Creates a downloader for the given index URLs.
    pub fn new(index_urls: Vec<String>) -> Downloader {
        Downloader {
            index_urls,
            existing_files: HashMap::new(),
        }
    }

    /// Returns the checksums of the files gathered so far.
    pub fn existing_files(&self) -> &HashMap<String, String> {
        &self.existing_files
    }

    /// Downloads the index from the first working URL, writes it to disk and
    /// returns it, like Go `updateIndex`.
    pub fn update_index(
        &self,
        client: &dyn HttpClient,
        download_dir: &Path,
        index_file: &str,
        platform: &str,
        verifier: Option<IndexVerifier<'_>>,
    ) -> Result<Index> {
        ensure_directory(download_dir)?;

        let mut last_error = None;
        for url in &self.index_urls {
            match self.get_index(client, url, platform, verifier) {
                Ok((index_data, index)) => {
                    let index_path = download_dir.join(index_file);
                    std::fs::write(&index_path, &index_data).map_err(|error| {
                        Error::Process(format!("write index file: {error}"))
                    })?;
                    set_public_read_exec(&index_path);
                    return Ok(index);
                }
                Err(error) => {
                    tracing::warn!(url, %error, "updates: failed to update index");
                    last_error = Some(format!("update index file from {url:?}: {error}"));
                }
            }
        }

        Err(Error::Process(format!(
            "all index URLs failed, last error: {}",
            last_error.unwrap_or_else(|| "no index URLs configured".to_string())
        )))
    }

    /// Downloads and parses one index, like Go `getIndex`.
    fn get_index(
        &self,
        client: &dyn HttpClient,
        url: &str,
        platform: &str,
        verifier: Option<IndexVerifier<'_>>,
    ) -> Result<(Vec<u8>, Index)> {
        let index_data = self
            .download_data(client, url)
            .map_err(|error| Error::Process(format!("GET index: {error}")))?;
        let index = Index::parse(&index_data, platform, verifier)
            .map_err(|error| Error::Process(format!("parse index: {error}")))?;
        Ok((index_data, index))
    }

    /// Gathers the checksums of the existing files in the directory.
    pub fn gather_existing_files(&mut self, dir: &Path) -> Result<()> {
        let mut stack = vec![dir.to_path_buf()];
        while let Some(current) = stack.pop() {
            let entries = std::fs::read_dir(&current)
                .map_err(|error| Error::Process(format!("searching for existing files: {error}")))?;
            for entry in entries {
                let entry = entry.map_err(|error| {
                    Error::Process(format!("searching for existing files: {error}"))
                })?;
                let path = entry.path();
                if path.is_dir() {
                    stack.push(path);
                    continue;
                }
                let data = std::fs::read(&path).map_err(|error| {
                    Error::Process(format!("failed to read file {}: {error}", path.display()))
                })?;
                let digest = Sha256::digest(&data);
                self.existing_files
                    .insert(hex::encode(digest), path.to_string_lossy().into_owned());
            }
        }
        Ok(())
    }

    /// Downloads and verifies all artifacts of the index into the directory,
    /// like Go `downloadArtifacts`.
    pub fn download_artifacts(
        &self,
        client: &dyn HttpClient,
        download_dir: &Path,
        index: &Index,
    ) -> Result<()> {
        ensure_directory(download_dir)?;

        for artifact in &index.artifacts {
            let destination = download_dir.join(&artifact.filename);

            // Copy the artifact from disk when the checksum is already known.
            if let Some(existing) = self.existing_files.get(&artifact.sha256) {
                let existing_path = PathBuf::from(existing);
                if existing_path == destination {
                    continue;
                }
                if copy_and_check_sha256_sum(
                    &existing_path,
                    &destination,
                    &artifact.sha256,
                    artifact.get_file_mode(),
                )
                .is_ok()
                {
                    continue;
                }
            }

            if artifact.urls.is_empty() {
                return Err(Error::Process(format!(
                    "artifact {} is missing download URLs",
                    artifact.filename
                )));
            }

            let mut artifact_data = None;
            let mut last_error = None;
            for url in &artifact.urls {
                match self.get_artifact(client, artifact, url) {
                    Ok(data) => {
                        artifact_data = Some(data);
                        break;
                    }
                    Err(error) => {
                        last_error =
                            Some(format!("update index file from {url:?}: {error}"));
                    }
                }
            }
            let Some(artifact_data) = artifact_data else {
                return Err(Error::Process(format!(
                    "all artifact URLs for {} failed, last error: {}",
                    artifact.filename,
                    last_error.unwrap_or_else(|| "no URLs".to_string())
                )));
            };

            let temporary = destination.with_extension("download");
            std::fs::write(&temporary, &artifact_data).map_err(|error| {
                Error::Process(format!(
                    "write {} to temp file: {error}",
                    artifact.filename
                ))
            })?;
            set_file_mode(&temporary, artifact.get_file_mode());
            std::fs::rename(&temporary, &destination).map_err(|error| {
                Error::Process(format!("rename {} after write: {error}", artifact.filename))
            })?;
            tracing::info!(file = %artifact.filename, "updates: downloaded and verified");
        }

        Ok(())
    }

    /// Downloads and verifies one artifact, like Go `getArtifact`.
    fn get_artifact(
        &self,
        client: &dyn HttpClient,
        artifact: &crate::Artifact,
        url: &str,
    ) -> Result<Vec<u8>> {
        let mut data = self
            .download_data(client, url)
            .map_err(|error| Error::Process(format!("GET artifact: {error}")))?;

        if !artifact.unpack.is_empty() {
            data = decompress(&artifact.unpack, &data)
                .map_err(|error| Error::Process(format!("decompress: {error}")))?;
        }

        check_sha256_sum(&data, &artifact.sha256)?;
        Ok(data)
    }

    /// Downloads data from a URL, like Go `downloadData`.
    pub fn download_data(
        &self,
        client: &dyn HttpClient,
        url: &str,
    ) -> std::result::Result<Vec<u8>, String> {
        client.get(url)
    }
}

/// Copies a file and verifies its checksum, like Go `copyAndCheckSHA256Sum`.
pub fn copy_and_check_sha256_sum(
    source: &Path,
    destination: &Path,
    sha256sum: &str,
    mode: u32,
) -> Result<()> {
    let data = std::fs::read(source)
        .map_err(|error| Error::Process(format!("read src file: {error}")))?;

    if !sha256sum.is_empty() {
        check_sha256_sum(&data, sha256sum)?;
    }

    let temporary = destination.with_extension("copy");
    std::fs::write(&temporary, &data)
        .map_err(|error| Error::Process(format!("write temp dst file: {error}")))?;
    set_file_mode(&temporary, mode);
    std::fs::rename(&temporary, destination)
        .map_err(|error| Error::Process(format!("rename dst file after write: {error}")))?;
    Ok(())
}

/// Verifies a file's SHA-256 against the *decompressed* bytes described by an
/// index artifact.
///
/// Mirrors store the original (compressed) bytes on disk, while index
/// checksums describe the unpacked content (see Go `Decompress` and the normal
/// `Downloader.getArtifact` path). Verifying the raw file directly therefore
/// fails for artifacts with an `Unpack` type. Use this helper when checking a
/// mirrored file against its index checksum so the check covers the same bytes
/// the checksum was computed over.
pub fn check_sha256_sum_file_unpacked(
    filename: impl AsRef<Path>,
    unpack: &str,
    sha256sum: &str,
) -> Result<()> {
    let data = std::fs::read(filename).map_err(Error::OpenFile)?;
    let data = if unpack.is_empty() {
        data
    } else {
        decompress(unpack, &data)?
    };
    check_sha256_sum(&data, sha256sum)
}

/// Creates the directory if it does not exist.
fn ensure_directory(dir: &Path) -> Result<()> {
    std::fs::create_dir_all(dir)
        .map_err(|_| Error::Process(format!("create download directory: {}", dir.display())))
}

/// Sets the file permissions on Unix.
fn set_file_mode(path: &Path, mode: u32) {
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        let _ = std::fs::set_permissions(path, std::fs::Permissions::from_mode(mode));
    }
    #[cfg(not(unix))]
    {
        let _ = (path, mode);
    }
}

/// Sets public read/exec permissions.
fn set_public_read_exec(path: &Path) {
    set_file_mode(path, 0o755);
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn user_agent_matches_go_shape() {
        let user_agent = default_user_agent();
        assert!(user_agent.starts_with("Portmaster ("), "{user_agent}");
        assert!(user_agent.contains(std::env::consts::OS));
    }

    #[test]
    fn unsupported_compression_is_rejected() {
        match decompress("xz", b"data") {
            Err(Error::Process(message)) => {
                assert_eq!(message, "unsupported compression type")
            }
            other => panic!("unexpected result: {other:?}"),
        }
    }

    #[test]
    fn gzip_decompresses_all_members() {
        use flate2::write::GzEncoder;
        use flate2::Compression;
        use std::io::Write;

        // Concatenated gzip members, as produced e.g. by `gzip -c a b > out.gz`.
        let mut combined = Vec::new();
        for part in [b"first".as_slice(), b"second".as_slice()] {
            let mut encoder = GzEncoder::new(Vec::new(), Compression::default());
            encoder.write_all(part).unwrap();
            combined.extend_from_slice(&encoder.finish().unwrap());
        }
        assert_eq!(decompress("gz", &combined).unwrap(), b"firstsecond");
    }

    #[test]
    fn unpacked_file_checksum_matches_decompressed_bytes() {
        use crate::check_sha256_sum_file;
        use flate2::write::GzEncoder;
        use flate2::Compression;
        use std::io::Write;

        let plain = b"the decompressed content";
        let expected = sha2::Sha256::digest(plain);
        let hash = hex::encode(expected);

        let mut encoder = GzEncoder::new(Vec::new(), Compression::default());
        encoder.write_all(plain).unwrap();
        let gzipped = encoder.finish().unwrap();

        let dir = std::env::temp_dir().join(format!(
            "pm-updates-unpacked-hash-{}-{}",
            std::process::id(),
            plain.len()
        ));
        let _ = std::fs::create_dir_all(&dir);
        let path = dir.join("artifact.gz");
        std::fs::write(&path, &gzipped).unwrap();

        assert!(check_sha256_sum_file_unpacked(&path, "gz", &hash).is_ok());
        // A plain checksum over the compressed bytes does not match, which is
        // the mismatch the mirror helper exists to avoid.
        assert!(check_sha256_sum_file(&path, &hash).is_err());
        let _ = std::fs::remove_dir_all(&dir);
    }
}
