//! Hashing of executable files, port of Go `service/process/executable.go`.

use std::fs::File;
use std::io::{self, Read};

/// Errors of executable hashing.
#[derive(Debug, thiserror::Error)]
pub enum HashError {
    #[error("io: {0}")]
    Io(#[from] io::Error),
    /// Go leaves the hasher `nil` for unknown algorithms, which panics on use.
    /// Rust reports an error instead.
    #[error("unsupported hash algorithm: {0}")]
    UnsupportedAlgorithm(String),
}

/// Size of the chunks the executable is read in.
const CHUNK_SIZE: usize = 64 * 1024;

/// Streams the file into `update`, like Go's `io.Copy(hasher, file)`.
fn feed(mut file: File, mut update: impl FnMut(&[u8])) -> io::Result<()> {
    let mut buffer = vec![0u8; CHUNK_SIZE];
    loop {
        let read = file.read(&mut buffer)?;
        if read == 0 {
            return Ok(());
        }
        update(&buffer[..read]);
    }
}

/// Hashes the file at `path` with the given algorithm.
///
/// Supported algorithms are `md5`, `sha1` and `sha256`; the result is the
/// lowercase hex encoding of the digest, exactly like Go's
/// `hex.EncodeToString`.
pub fn hash_file(path: &str, algorithm: &str) -> Result<String, HashError> {
    let file = File::open(path)?;

    match algorithm {
        "md5" => {
            use md5::{Digest, Md5};
            let mut hasher = Md5::new();
            feed(file, |chunk| hasher.update(chunk))?;
            Ok(hex::encode(hasher.finalize()))
        }
        "sha1" => {
            use sha1::{Digest, Sha1};
            let mut hasher = Sha1::new();
            feed(file, |chunk| hasher.update(chunk))?;
            Ok(hex::encode(hasher.finalize()))
        }
        "sha256" => {
            use sha2::{Digest, Sha256};
            let mut hasher = Sha256::new();
            feed(file, |chunk| hasher.update(chunk))?;
            Ok(hex::encode(hasher.finalize()))
        }
        other => Err(HashError::UnsupportedAlgorithm(other.to_string())),
    }
}
