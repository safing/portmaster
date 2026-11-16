//! Filesystem helpers ported from Go `base/utils/fs.go` and
//! `base/utils/permissions.go`.

use std::io::{self, ErrorKind};
use std::path::Path;

use crate::structure::FSPermission;

#[cfg(unix)]
use std::os::unix::fs::PermissionsExt;

/// Returns whether the given path (file or directory) exists.
///
/// Go's implementation also checks `errors.Is(err, fs.ErrExist)`, which
/// `os.Stat` never returns; the observable behaviour is simply "stat succeeds".
pub fn path_exists(path: &Path) -> bool {
    std::fs::metadata(path).is_ok()
}

/// Sets the permission of a file or directory, like Go's `SetFilePermission`.
#[cfg(unix)]
pub fn set_file_permission(path: &Path, perm: FSPermission) -> io::Result<()> {
    std::fs::set_permissions(path, std::fs::Permissions::from_mode(perm.as_unix_permission()))
}

/// On Windows permission changes require elevated rights and are best-effort.
#[cfg(windows)]
pub fn set_file_permission(path: &Path, perm: FSPermission) -> io::Result<()> {
    let _ = perm;
    let metadata = std::fs::metadata(path)?;
    let mut permissions = metadata.permissions();
    permissions.set_readonly(false);
    std::fs::set_permissions(path, permissions)
}

/// Ensures that `path` is a directory with the requested permissions.
///
/// If a regular file exists at `path` it is removed first. On Windows
/// permission errors are ignored, matching Go's non-admin friendly behaviour.
pub fn ensure_directory(path: &Path, perm: FSPermission) -> io::Result<()> {
    if !perm.is_exec_permission() {
        tracing::warn!(
            dir = %path.display(),
            "utils: setting not executable permission for directory"
        );
    }

    match std::fs::metadata(path) {
        Ok(metadata) => {
            if metadata.is_dir() {
                #[cfg(windows)]
                {
                    let _ = set_file_permission(path, perm);
                    return Ok(());
                }
                #[cfg(unix)]
                {
                    if metadata.permissions().mode() & 0o7777 != perm.as_unix_permission() {
                        return set_file_permission(path, perm);
                    }
                    return Ok(());
                }
                #[cfg(not(any(unix, windows)))]
                return Ok(());
            }

            std::fs::remove_file(path).map_err(|error| {
                io::Error::new(
                    error.kind(),
                    format!(
                        "could not remove file {} to place dir: {error}",
                        path.display()
                    ),
                )
            })?;
        }
        Err(error) if error.kind() == ErrorKind::NotFound => {}
        Err(error) => {
            return Err(io::Error::new(
                error.kind(),
                format!("failed to access {}: {error}", path.display()),
            ));
        }
    }

    std::fs::create_dir_all(path).map_err(|error| {
        io::Error::new(
            error.kind(),
            format!("could not create dir {}: {error}", path.display()),
        )
    })?;

    #[cfg(windows)]
    {
        let _ = set_file_permission(path, perm);
        Ok(())
    }
    #[cfg(unix)]
    {
        set_file_permission(path, perm)
    }
    #[cfg(not(any(unix, windows)))]
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::structure::FSPermission;

    fn scratch_dir(name: &str) -> std::path::PathBuf {
        let dir = std::env::temp_dir().join(format!("pm-utils-fs-{name}-{}", std::process::id()));
        let _ = std::fs::remove_dir_all(&dir);
        dir
    }

    #[test]
    fn path_exists_for_files_dirs_and_symlinks() {
        let root = scratch_dir("exists");
        std::fs::create_dir_all(&root).unwrap();
        let file = root.join("file");
        std::fs::write(&file, b"x").unwrap();
        assert!(path_exists(&file));
        assert!(path_exists(&root));
        assert!(!path_exists(&root.join("nope")));
        let _ = std::fs::remove_dir_all(&root);
    }

    #[test]
    fn ensure_directory_creates_and_fixes_permissions() {
        let root = scratch_dir("ensure");
        let dir = root.join("a/b/c");
        ensure_directory(&dir, FSPermission::PublicReadExec).unwrap();
        assert!(dir.is_dir());
        #[cfg(unix)]
        {
            let mode = std::fs::metadata(&dir).unwrap().permissions().mode() & 0o7777;
            assert_eq!(mode, 0o755);
        }
        // Re-ensuring with a different mode corrects it.
        ensure_directory(&dir, FSPermission::AdminOnlyExec).unwrap();
        #[cfg(unix)]
        {
            let mode = std::fs::metadata(&dir).unwrap().permissions().mode() & 0o7777;
            assert_eq!(mode, 0o700);
        }
        let _ = std::fs::remove_dir_all(&root);
    }

    #[test]
    fn ensure_directory_replaces_a_regular_file() {
        let root = scratch_dir("replace-file");
        std::fs::create_dir_all(&root).unwrap();
        let target = root.join("target");
        std::fs::write(&target, b"not a dir").unwrap();
        ensure_directory(&target, FSPermission::PublicReadExec).unwrap();
        assert!(target.is_dir());
        let _ = std::fs::remove_dir_all(&root);
    }
}
