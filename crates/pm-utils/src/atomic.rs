//! Atomic file creation, copying and replacement, ported from Go
//! `base/utils/atomic.go`.

use std::fs::File;
use std::io::{self, Read};
use std::path::{Path, PathBuf};

use crate::renameio;

/// Additional options for [`create_atomic`] and friends.
#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct AtomicFileOptions {
    /// Permission mode for the new file. `None` (Go's `0`) means `0600` for
    /// [`create_atomic`]; the copy helpers try to inherit a source/destination
    /// mode instead.
    pub mode: Option<u32>,

    /// Directory used for the temporary file. `None` lets [`renameio::temp_dir`]
    /// choose a directory on the same filesystem as the destination.
    pub temp_dir: Option<PathBuf>,
}

fn contextual(error: io::Error, message: String) -> io::Error {
    io::Error::new(error.kind(), format!("{message}: {error}"))
}

/// Creates or overwrites `dest` atomically using data from `reader`.
///
/// Even on a power outage `dest` is never a zero-length file: it contains
/// either the previous data or the new data.
pub fn create_atomic<P: AsRef<Path>, R: Read>(
    dest: P,
    mut reader: R,
    opts: Option<&AtomicFileOptions>,
) -> io::Result<()> {
    let dest = dest.as_ref();
    let default = AtomicFileOptions::default();
    let opts = opts.unwrap_or(&default);

    let mut pending = renameio::temp_file(opts.temp_dir.as_deref(), dest).map_err(|error| {
        contextual(error, "failed to create temp file".to_string())
    })?;

    if let Some(mode) = opts.mode {
        if mode != 0 {
            pending.set_mode(mode).map_err(|error| {
                contextual(error, "failed to update mode bits of temp file".to_string())
            })?;
        }
    }

    io::copy(&mut reader, &mut pending)
        .map_err(|error| contextual(error, "failed to copy source file".to_string()))?;

    pending.close_atomically_replace().map_err(|error| {
        // Go's error message drops the underlying error here; keep the wording.
        let _ = error;
        io::Error::other(format!("failed to rename temp file to {:?}", dest))
    })
}

/// Like [`create_atomic`], but copies content from `src` to `dest`.
///
/// When no mode is given, the source file's mode is used.
pub fn copy_file_atomic<P: AsRef<Path>, Q: AsRef<Path>>(
    dest: P,
    src: Q,
    opts: Option<&AtomicFileOptions>,
) -> io::Result<()> {
    let src = src.as_ref();
    let default = AtomicFileOptions::default();
    let opts = opts.unwrap_or(&default);

    let options = if opts.mode.is_none() || opts.mode == Some(0) {
        let metadata = std::fs::metadata(src)?;
        #[cfg(unix)]
        let mode = {
            use std::os::unix::fs::PermissionsExt;
            metadata.permissions().mode() & 0o7777
        };
        #[cfg(not(unix))]
        let mode = 0o600;
        AtomicFileOptions {
            mode: Some(mode),
            temp_dir: opts.temp_dir.clone(),
        }
    } else {
        opts.clone()
    };

    let mut file = File::open(src)?;
    create_atomic(dest, &mut file, Some(&options))
}

/// Replaces `dest` with `src`.
///
/// If `dest` exists its mode is reused; otherwise `src`'s mode is used.
pub fn replace_file_atomic<P: AsRef<Path>, Q: AsRef<Path>>(
    dest: P,
    src: Q,
    opts: Option<&AtomicFileOptions>,
) -> io::Result<()> {
    let dest = dest.as_ref();
    let default = AtomicFileOptions::default();
    let opts = opts.unwrap_or(&default);

    let options = if opts.mode.is_none() || opts.mode == Some(0) {
        match std::fs::metadata(dest) {
            Ok(metadata) => {
                #[cfg(unix)]
                let mode = {
                    use std::os::unix::fs::PermissionsExt;
                    metadata.permissions().mode() & 0o7777
                };
                #[cfg(not(unix))]
                let mode = 0o600;
                AtomicFileOptions {
                    mode: Some(mode),
                    temp_dir: opts.temp_dir.clone(),
                }
            }
            Err(error) if error.kind() == io::ErrorKind::NotFound => opts.clone(),
            Err(error) => return Err(error),
        }
    } else {
        opts.clone()
    };

    copy_file_atomic(dest, src, Some(&options))
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::fs::Permissions;

    fn scratch_dir(name: &str) -> PathBuf {
        let dir = std::env::temp_dir().join(format!("pm-utils-atomic-{name}-{}", std::process::id()));
        let _ = std::fs::remove_dir_all(&dir);
        std::fs::create_dir_all(&dir).unwrap();
        dir
    }

    #[test]
    fn create_atomic_writes_bytes_and_uses_0600_by_default() {
        let dir = scratch_dir("create");
        let dest = dir.join("config.json");
        let payload = b"{\"a\":1}";
        create_atomic(&dest, &payload[..], None).unwrap();
        assert_eq!(std::fs::read(&dest).unwrap(), payload);

        #[cfg(unix)]
        {
            use std::os::unix::fs::PermissionsExt;
            let mode = std::fs::metadata(&dest).unwrap().permissions().mode() & 0o7777;
            assert_eq!(mode, 0o600);
        }
        let _ = std::fs::remove_dir_all(&dir);
    }

    #[test]
    fn create_atomic_honours_explicit_mode_and_temp_dir() {
        let dir = scratch_dir("mode");
        let temp = dir.join("tmp");
        std::fs::create_dir(&temp).unwrap();
        let dest = dir.join("out");
        create_atomic(
            &dest,
            &b"data"[..],
            Some(&AtomicFileOptions {
                mode: Some(0o640),
                temp_dir: Some(temp.clone()),
            }),
        )
        .unwrap();
        assert_eq!(std::fs::read(&dest).unwrap(), b"data");
        #[cfg(unix)]
        {
            use std::os::unix::fs::PermissionsExt;
            let mode = std::fs::metadata(&dest).unwrap().permissions().mode() & 0o7777;
            assert_eq!(mode, 0o640);
        }
        let _ = std::fs::remove_dir_all(&dir);
    }

    #[test]
    fn create_atomic_replaces_existing_file_without_truncating_on_failure() {
        let dir = scratch_dir("failure");
        let dest = dir.join("keep");
        std::fs::write(&dest, b"original").unwrap();

        struct FailingReader;
        impl Read for FailingReader {
            fn read(&mut self, _buf: &mut [u8]) -> io::Result<usize> {
                Err(io::Error::other("boom"))
            }
        }
        let error = create_atomic(&dest, FailingReader, None).unwrap_err();
        assert!(error.to_string().contains("failed to copy source file"));
        assert_eq!(std::fs::read(&dest).unwrap(), b"original");

        // No stray temporary file is left behind.
        assert_eq!(std::fs::read_dir(&dir).unwrap().count(), 1);
        let _ = std::fs::remove_dir_all(&dir);
    }

    #[test]
    fn copy_file_atomic_inherits_source_mode() {
        let dir = scratch_dir("copy");
        let src = dir.join("src");
        let dest = dir.join("dest");
        std::fs::write(&src, b"payload").unwrap();
        #[cfg(unix)]
        {
            use std::os::unix::fs::PermissionsExt;
            std::fs::set_permissions(&src, Permissions::from_mode(0o644)).unwrap();
        }
        copy_file_atomic(&dest, &src, None).unwrap();
        assert_eq!(std::fs::read(&dest).unwrap(), b"payload");
        #[cfg(unix)]
        {
            use std::os::unix::fs::PermissionsExt;
            let mode = std::fs::metadata(&dest).unwrap().permissions().mode() & 0o7777;
            assert_eq!(mode, 0o644);
        }
        let _ = std::fs::remove_dir_all(&dir);
    }

    #[test]
    fn replace_file_atomic_keeps_existing_dest_mode() {
        let dir = scratch_dir("replace");
        let src = dir.join("src");
        let dest = dir.join("dest");
        std::fs::write(&src, b"new").unwrap();
        std::fs::write(&dest, b"old").unwrap();
        #[cfg(unix)]
        {
            use std::os::unix::fs::PermissionsExt;
            std::fs::set_permissions(&dest, Permissions::from_mode(0o604)).unwrap();
        }
        replace_file_atomic(&dest, &src, None).unwrap();
        assert_eq!(std::fs::read(&dest).unwrap(), b"new");
        #[cfg(unix)]
        {
            use std::os::unix::fs::PermissionsExt;
            let mode = std::fs::metadata(&dest).unwrap().permissions().mode() & 0o7777;
            assert_eq!(mode, 0o604);
        }
        let _ = std::fs::remove_dir_all(&dir);
    }

    #[test]
    fn replace_file_atomic_uses_source_mode_when_dest_is_missing() {
        let dir = scratch_dir("replace-missing");
        let src = dir.join("src");
        let dest = dir.join("dest");
        std::fs::write(&src, b"new").unwrap();
        #[cfg(unix)]
        {
            use std::os::unix::fs::PermissionsExt;
            std::fs::set_permissions(&src, Permissions::from_mode(0o640)).unwrap();
        }
        replace_file_atomic(&dest, &src, None).unwrap();
        #[cfg(unix)]
        {
            use std::os::unix::fs::PermissionsExt;
            let mode = std::fs::metadata(&dest).unwrap().permissions().mode() & 0o7777;
            assert_eq!(mode, 0o640);
        }
        let _ = std::fs::remove_dir_all(&dir);
    }

    #[test]
    fn copy_file_atomic_reports_missing_source() {
        let dir = scratch_dir("missing-src");
        let error = copy_file_atomic(dir.join("dest"), dir.join("nope"), None).unwrap_err();
        assert_eq!(error.kind(), io::ErrorKind::NotFound);
        let _ = std::fs::remove_dir_all(&dir);
    }
}
