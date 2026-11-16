//! Atomic file and symlink replacement, ported from Go `base/utils/renameio`.
//!
//! This is a fork of `github.com/google/renameio` (commit 353f819). The key
//! properties preserved here are:
//! * the temporary file is created in the same directory as the destination
//!   (or in an explicitly provided directory) so `rename(2)` cannot cross a
//!   mount point;
//! * the temporary file is created with mode `0600`;
//! * `sync_all` (fsync) is called before `rename`, so a crash can never expose
//!   a zero-length destination;
//! * a failed operation cleans up its temporary file, a successful one does
//!   not remove the (now renamed) path.

use std::fs::{File, OpenOptions, Permissions};
use std::io::{self, Write};
use std::path::{Path, PathBuf};
use std::sync::atomic::{AtomicU64, Ordering};
use std::time::{SystemTime, UNIX_EPOCH};

#[cfg(unix)]
use std::os::unix::fs::{OpenOptionsExt, PermissionsExt};

static COUNTER: AtomicU64 = AtomicU64::new(0);

fn unique_suffix() -> String {
    let nanos = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|elapsed| elapsed.as_nanos())
        .unwrap_or(0);
    let sequence = COUNTER.fetch_add(1, Ordering::Relaxed);
    format!("{nanos:x}-{}-{sequence:x}", std::process::id())
}

fn create_new_file(dir: &Path, prefix: &str) -> io::Result<(File, PathBuf)> {
    let mut options = OpenOptions::new();
    options.write(true).create_new(true);
    #[cfg(unix)]
    options.mode(0o600);
    for attempt in 0..64u64 {
        let path = dir.join(format!("{prefix}{}-{attempt:x}", unique_suffix()));
        match options.open(&path) {
            Ok(file) => return Ok((file, path)),
            Err(error) if error.kind() == io::ErrorKind::AlreadyExists => continue,
            Err(error) => return Err(error),
        }
    }
    Err(io::Error::new(
        io::ErrorKind::AlreadyExists,
        "could not create a unique temporary file",
    ))
}

/// Owns a probe path and removes it on drop, like Go's `defer os.Remove`.
struct TempPathGuard(PathBuf);

impl TempPathGuard {
    fn path(&self) -> &Path {
        &self.0
    }
}

impl Drop for TempPathGuard {
    fn drop(&mut self) {
        let _ = std::fs::remove_file(&self.0);
    }
}

/// Chooses a temporary directory suitable for atomically replacing files in
/// `dest`, like Go's `renameio.TempDir`.
///
/// If `dir` is provided it always wins. Otherwise the system temporary
/// directory is probed by creating a file there and renaming it into the
/// destination directory; if that fails, the destination's own directory is
/// used as the fallback.
pub fn temp_dir(dir: Option<&Path>, dest: &Path) -> PathBuf {
    if let Some(dir) = dir {
        return dir.to_path_buf();
    }

    let fallback = dest
        .parent()
        .filter(|parent| !parent.as_os_str().is_empty())
        .unwrap_or_else(|| Path::new("."))
        .to_path_buf();
    let base = dest
        .file_name()
        .map(|name| name.to_string_lossy().into_owned())
        .unwrap_or_else(|| "renameio-TempDir".to_string());
    let prefix = format!(".{base}");
    let tmp_root = std::env::temp_dir();

    let Ok((source_file, source_path)) = create_new_file(&tmp_root, &prefix) else {
        return fallback;
    };
    drop(source_file);
    let source_guard = TempPathGuard(source_path);

    let Ok((destination_file, destination_path)) = create_new_file(&fallback, &prefix) else {
        return fallback;
    };
    drop(destination_file);
    let destination_guard = TempPathGuard(destination_path);

    let cross_mount_ok = std::fs::rename(source_guard.path(), destination_guard.path()).is_ok();
    if cross_mount_ok {
        tmp_root
    } else {
        fallback
    }
}

/// A temporary file waiting to replace a destination path.
///
/// Dropping an unfinished pending file removes the temporary file, which is a
/// safe Rust equivalent of Go's explicit `defer tmpFile.Cleanup()`.
pub struct PendingFile {
    file: Option<File>,
    temp_path: PathBuf,
    dest: PathBuf,
    done: bool,
}

impl PendingFile {
    /// Removes the temporary file unless the atomic replace already succeeded.
    pub fn cleanup(&mut self) -> io::Result<()> {
        if self.done {
            return Ok(());
        }
        self.done = true;

        drop(self.file.take());
        match std::fs::remove_file(&self.temp_path) {
            Ok(()) => Ok(()),
            Err(error) if error.kind() == io::ErrorKind::NotFound => Ok(()),
            Err(error) => Err(error),
        }
    }

    /// Closes, fsyncs and atomically renames the temporary file to `dest`.
    pub fn close_atomically_replace(&mut self) -> io::Result<()> {
        if self.done {
            return Ok(());
        }
        let file = self
            .file
            .take()
            .ok_or_else(|| io::Error::other("pending file is already closed"))?;
        file.sync_all()?;
        drop(file);
        std::fs::rename(&self.temp_path, &self.dest)?;
        self.done = true;
        Ok(())
    }

    /// Path of the temporary file, like Go's `PendingFile.Name()`.
    pub fn name(&self) -> &Path {
        &self.temp_path
    }

    /// Path of the final destination.
    pub fn dest(&self) -> &Path {
        &self.dest
    }

    /// Sets the permissions of the temporary file, like `File.Chmod`.
    pub fn set_mode(&self, mode: u32) -> io::Result<()> {
        #[cfg(unix)]
        {
            self.file
                .as_ref()
                .ok_or_else(|| io::Error::other("pending file is closed"))?
                .set_permissions(Permissions::from_mode(mode))
        }
        #[cfg(not(unix))]
        {
            let _ = mode;
            Ok(())
        }
    }
}

impl Write for PendingFile {
    fn write(&mut self, buf: &[u8]) -> io::Result<usize> {
        self.file
            .as_mut()
            .ok_or_else(|| io::Error::other("pending file is closed"))?
            .write(buf)
    }

    fn flush(&mut self) -> io::Result<()> {
        self.file.as_mut().map_or(Ok(()), File::flush)
    }
}

impl Drop for PendingFile {
    fn drop(&mut self) {
        let _ = self.cleanup();
    }
}

/// Creates a temporary file for atomically creating or replacing `path`.
///
/// `dir` selects the temporary directory; `None` uses [`temp_dir`]. The file is
/// created with mode `0600` on Unix.
pub fn temp_file(dir: Option<&Path>, path: &Path) -> io::Result<PendingFile> {
    let chosen = temp_dir(dir, path);
    let base = path
        .file_name()
        .map(|name| name.to_string_lossy().into_owned())
        .unwrap_or_else(|| "renameio-TempFile".to_string());
    let (file, temp_path) = create_new_file(&chosen, &format!(".{base}"))?;
    Ok(PendingFile {
        file: Some(file),
        temp_path,
        dest: path.to_path_buf(),
        done: false,
    })
}

/// Mirrors `os.WriteFile`, but replaces the destination atomically.
///
/// Permissions are applied before any data is written so sensitive content is
/// never briefly world-readable. On Unix `mode` is a POSIX permission mode.
pub fn write_file(path: &Path, data: &[u8], mode: u32) -> io::Result<()> {
    let mut pending = temp_file(None, path)?;
    pending.set_mode(mode)?;
    pending.write_all(data)?;
    pending.close_atomically_replace()
}

/// Atomically replaces a symlink at `newname`, following Go's `renameio.Symlink`.
#[cfg(unix)]
pub fn symlink(oldname: &Path, newname: &Path) -> io::Result<()> {
    match std::os::unix::fs::symlink(oldname, newname) {
        Ok(()) => return Ok(()),
        Err(error) if error.kind() != io::ErrorKind::AlreadyExists => return Err(error),
        Err(_) => {}
    }

    let parent = newname
        .parent()
        .filter(|parent| !parent.as_os_str().is_empty())
        .unwrap_or_else(|| Path::new("."));
    let base = newname
        .file_name()
        .map(|name| name.to_string_lossy().into_owned())
        .unwrap_or_else(|| "renameio-Symlink".to_string());

    let mut scratch = None;
    for attempt in 0..64u64 {
        let candidate = parent.join(format!(".{base}{}-{attempt:x}", unique_suffix()));
        match std::fs::create_dir(&candidate) {
            Ok(()) => {
                scratch = Some(candidate);
                break;
            }
            Err(error) if error.kind() == io::ErrorKind::AlreadyExists => continue,
            Err(error) => return Err(error),
        }
    }
    let scratch = scratch.ok_or_else(|| {
        io::Error::new(
            io::ErrorKind::AlreadyExists,
            "could not create a temporary directory",
        )
    })?;

    let temporary_link = scratch.join("tmp.symlink");
    if let Err(error) = std::os::unix::fs::symlink(oldname, &temporary_link) {
        let _ = std::fs::remove_dir_all(&scratch);
        return Err(error);
    }
    if let Err(error) = std::fs::rename(&temporary_link, newname) {
        let _ = std::fs::remove_dir_all(&scratch);
        return Err(error);
    }
    std::fs::remove_dir(&scratch)
}

/// Windows counterpart of [`symlink`]; requires privileged symlink creation.
#[cfg(windows)]
pub fn symlink(oldname: &Path, newname: &Path) -> io::Result<()> {
    if std::os::windows::fs::symlink_file(oldname, newname).is_ok() {
        return Ok(());
    }
    let parent = newname
        .parent()
        .filter(|parent| !parent.as_os_str().is_empty())
        .unwrap_or_else(|| Path::new("."));
    let scratch = parent.join(format!(".renameio-{}", unique_suffix()));
    std::fs::create_dir(&scratch)?;
    let temporary_link = scratch.join("tmp.symlink");
    std::os::windows::fs::symlink_file(oldname, &temporary_link)?;
    std::fs::rename(&temporary_link, newname)?;
    std::fs::remove_dir(&scratch)
}

#[cfg(test)]
mod tests {
    use super::*;

    fn scratch_dir(name: &str) -> PathBuf {
        let dir = std::env::temp_dir().join(format!(
            "pm-utils-renameio-{name}-{}-{}",
            std::process::id(),
            COUNTER.fetch_add(1, Ordering::Relaxed)
        ));
        let _ = std::fs::remove_dir_all(&dir);
        std::fs::create_dir_all(&dir).unwrap();
        dir
    }

    #[test]
    fn write_file_creates_file_with_mode_and_exact_bytes() {
        let dir = scratch_dir("write");
        let path = dir.join("data.json");
        let data = b"{\n  \"key\": 42\n}\n";
        write_file(&path, data, 0o600).unwrap();
        assert_eq!(std::fs::read(&path).unwrap(), data);

        #[cfg(unix)]
        {
            let mode = std::fs::metadata(&path).unwrap().permissions().mode() & 0o7777;
            assert_eq!(mode, 0o600);
        }
        let leftovers: Vec<_> = std::fs::read_dir(&dir)
            .unwrap()
            .map(|entry| entry.unwrap().file_name())
            .collect();
        assert_eq!(leftovers, vec![std::ffi::OsString::from("data.json")]);

        let _ = std::fs::remove_dir_all(&dir);
    }

    #[test]
    fn write_file_replaces_existing_content() {
        let dir = scratch_dir("replace");
        let path = dir.join("config.json");
        std::fs::write(&path, b"old and much longer content").unwrap();
        write_file(&path, b"new", 0o640).unwrap();
        assert_eq!(std::fs::read(&path).unwrap(), b"new");

        #[cfg(unix)]
        {
            let mode = std::fs::metadata(&path).unwrap().permissions().mode() & 0o7777;
            assert_eq!(mode, 0o640);
        }
        let _ = std::fs::remove_dir_all(&dir);
    }

    #[test]
    fn cleanup_removes_temp_file_on_failure() {
        let dir = scratch_dir("cleanup");
        let dest = dir.join("dest");
        let mut pending = temp_file(Some(&dir), &dest).unwrap();
        let temp_path = pending.name().to_path_buf();
        pending.write_all(b"partial").unwrap();
        assert!(temp_path.exists());
        pending.cleanup().unwrap();
        assert!(!temp_path.exists());
        assert!(!dest.exists());
        pending.cleanup().unwrap();
        let _ = std::fs::remove_dir_all(&dir);
    }

    #[test]
    fn drop_cleans_up_unfinished_pending_file() {
        let dir = scratch_dir("drop");
        let dest = dir.join("dest");
        let temp_path = {
            let mut pending = temp_file(Some(&dir), &dest).unwrap();
            let path = pending.name().to_path_buf();
            pending.write_all(b"partial").unwrap();
            path
        };
        assert!(!temp_path.exists());
        let _ = std::fs::remove_dir_all(&dir);
    }

    #[test]
    fn temp_dir_uses_explicit_dir_and_detects_a_usable_default() {
        let dir = scratch_dir("tempdir");
        let dest = dir.join("file");
        assert_eq!(temp_dir(Some(&dir), &dest), dir);
        let chosen = temp_dir(None, &dest);
        assert!(chosen.is_dir());
        let _ = std::fs::remove_dir_all(&dir);
    }

    #[cfg(unix)]
    #[test]
    fn symlink_creates_and_atomically_replaces() {
        let dir = scratch_dir("symlink");
        let link = dir.join("current");
        let first = dir.join("first");
        let second = dir.join("second");
        std::fs::write(&first, b"1").unwrap();
        std::fs::write(&second, b"2").unwrap();

        symlink(&first, &link).unwrap();
        assert_eq!(std::fs::read_link(&link).unwrap(), first);
        symlink(&second, &link).unwrap();
        assert_eq!(std::fs::read_link(&link).unwrap(), second);
        assert_eq!(std::fs::read(&link).unwrap(), b"2");
        let _ = std::fs::remove_dir_all(&dir);
    }
}
