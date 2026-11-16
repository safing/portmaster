//! Directory structure with per-directory permissions, ported from Go
//! `base/utils/structure.go`.

use std::collections::HashMap;
use std::io::{self, ErrorKind};
use std::path::{Path, PathBuf};
use std::sync::{Arc, Mutex, MutexGuard, Weak};

use crate::fs::ensure_directory;

/// Filesystem permission set, ported from Go's `FSPermission`.
#[repr(u8)]
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum FSPermission {
    /// Go: `AdminOnlyPermission` (`0600`).
    AdminOnly = 0,
    /// Go: `AdminOnlyExecPermission` (`0700`).
    AdminOnlyExec = 1,
    /// Go: `PublicReadPermission` (`0644`).
    PublicRead = 2,
    /// Go: `PublicReadExecPermission` (`0755`).
    PublicReadExec = 3,
    /// Go: `PublicWritePermission` (`0666`).
    PublicWrite = 4,
    /// Go: `PublicWriteExecPermission` (`0777`).
    PublicWriteExec = 5,
}

impl FSPermission {
    /// Returns the Unix mode for this permission set, like Go's
    /// `AsUnixPermission`. Unknown values map to `0`.
    pub fn as_unix_permission(self) -> u32 {
        match self {
            FSPermission::AdminOnly => 0o600,
            FSPermission::AdminOnlyExec => 0o700,
            FSPermission::PublicRead => 0o644,
            FSPermission::PublicReadExec => 0o755,
            FSPermission::PublicWrite => 0o666,
            FSPermission::PublicWriteExec => 0o777,
        }
    }

    /// Returns whether this permission set includes the execute bit.
    pub fn is_exec_permission(self) -> bool {
        matches!(
            self,
            FSPermission::AdminOnlyExec
                | FSPermission::PublicReadExec
                | FSPermission::PublicWriteExec
        )
    }
}

struct Node {
    perm: FSPermission,
    children: HashMap<String, Arc<DirStructure>>,
}

/// A directory tree whose permissions are enforced on disk.
///
/// The Go original embeds a `sync.Mutex`; here every node owns its own mutex,
/// which gives the same mutual exclusion without a shared lock object.
pub struct DirStructure {
    path: PathBuf,
    dir: String,
    parent: Weak<DirStructure>,
    inner: Mutex<Node>,
}

impl DirStructure {
    /// Creates a root node.
    pub fn new(path: impl Into<PathBuf>, perm: FSPermission) -> Arc<DirStructure> {
        Arc::new(DirStructure {
            path: path.into(),
            dir: String::new(),
            parent: Weak::new(),
            inner: Mutex::new(Node {
                perm,
                children: HashMap::new(),
            }),
        })
    }

    fn lock(&self) -> MutexGuard<'_, Node> {
        self.inner
            .lock()
            .unwrap_or_else(|poisoned| poisoned.into_inner())
    }

    /// Returns the absolute path of this node.
    pub fn path(&self) -> &Path {
        &self.path
    }

    /// Returns the directory name of this node (empty for the root).
    pub fn dir(&self) -> &str {
        &self.dir
    }

    /// Returns the configured permission of this node.
    pub fn permission(&self) -> FSPermission {
        self.lock().perm
    }

    /// Returns the parent node, if this is not the root.
    pub fn parent(&self) -> Option<Arc<DirStructure>> {
        self.parent.upgrade()
    }

    /// Adds a child directory, or updates the permissions of an existing one.
    pub fn child_dir(self: &Arc<Self>, dir_name: &str, perm: FSPermission) -> Arc<DirStructure> {
        let mut inner = self.lock();
        if let Some(child) = inner.children.get(dir_name) {
            child.lock().perm = perm;
            return Arc::clone(child);
        }

        let child = Arc::new(DirStructure {
            path: self.path.join(dir_name),
            dir: dir_name.to_string(),
            parent: Arc::downgrade(self),
            inner: Mutex::new(Node {
                perm,
                children: HashMap::new(),
            }),
        });
        inner.children.insert(dir_name.to_string(), Arc::clone(&child));
        child
    }

    /// Ensures the whole defined structure from the root exists.
    pub fn ensure(&self) -> io::Result<()> {
        self.ensure_abs_path(&self.path)
    }

    /// Ensures the structure plus a path relative to this node exists.
    pub fn ensure_rel_path(&self, dir_path: impl AsRef<Path>) -> io::Result<()> {
        let target = self.path.join(dir_path);
        self.ensure_abs_path(&target)
    }

    /// Ensures the structure plus the given relative directory names exist.
    pub fn ensure_rel_dir(&self, dir_names: &[&str]) -> io::Result<()> {
        let mut target = self.path.clone();
        for name in dir_names {
            target.push(name);
        }
        self.ensure_abs_path(&target)
    }

    /// Ensures the structure up to an absolute path exists.
    ///
    /// Paths outside the root's scope are rejected, like Go's
    /// `EnsureAbsPath`. The walk always starts at the root.
    pub fn ensure_abs_path(&self, dir_path: &Path) -> io::Result<()> {
        if let Some(parent) = self.parent.upgrade() {
            return parent.ensure_abs_path(dir_path);
        }

        if dir_path == self.path {
            return self.ensure_chain(&[]);
        }

        let base = self.path.to_string_lossy();
        let mut slashed = base.into_owned();
        if !slashed.ends_with(std::path::MAIN_SEPARATOR) {
            slashed.push(std::path::MAIN_SEPARATOR);
        }
        let target = dir_path.to_string_lossy();
        if !target.starts_with(&slashed) {
            return Err(io::Error::new(
                ErrorKind::InvalidInput,
                format!(
                    "path \"{}\" is outside of DirStructure scope",
                    dir_path.display()
                ),
            ));
        }

        let relative = &target[slashed.len()..];
        let components: Vec<&str> = relative
            .split('/')
            .filter(|component| !component.is_empty())
            .collect();
        self.ensure_chain(&components)
    }

    fn ensure_chain(&self, dirs: &[&str]) -> io::Result<()> {
        let (perm, child) = {
            let inner = self.lock();
            (inner.perm, dirs.first().and_then(|name| inner.children.get(*name)).cloned())
        };

        ensure_directory(&self.path, perm)?;

        let Some(first) = dirs.first() else {
            return Ok(());
        };
        let Some(child) = child else {
            // Past the defined structure: enforce this node's permission on
            // every remaining directory.
            let mut path = self.path.clone();
            for dir in dirs {
                path.push(dir);
                ensure_directory(&path, perm)?;
            }
            return Ok(());
        };
        debug_assert_eq!(*first, child.dir());
        child.ensure_chain(&dirs[1..])
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn scratch_dir(name: &str) -> PathBuf {
        let dir = std::env::temp_dir().join(format!("pm-utils-structure-{name}-{}", std::process::id()));
        let _ = std::fs::remove_dir_all(&dir);
        dir
    }

    #[cfg(unix)]
    fn mode_of(path: &Path) -> u32 {
        use std::os::unix::fs::PermissionsExt;
        std::fs::metadata(path).unwrap().permissions().mode() & 0o7777
    }

    #[test]
    fn permission_conversion_matches_go() {
        assert_eq!(FSPermission::AdminOnly.as_unix_permission(), 0o600);
        assert_eq!(FSPermission::AdminOnlyExec.as_unix_permission(), 0o700);
        assert_eq!(FSPermission::PublicRead.as_unix_permission(), 0o644);
        assert_eq!(FSPermission::PublicReadExec.as_unix_permission(), 0o755);
        assert_eq!(FSPermission::PublicWrite.as_unix_permission(), 0o666);
        assert_eq!(FSPermission::PublicWriteExec.as_unix_permission(), 0o777);

        assert!(!FSPermission::AdminOnly.is_exec_permission());
        assert!(FSPermission::AdminOnlyExec.is_exec_permission());
        assert!(!FSPermission::PublicRead.is_exec_permission());
        assert!(FSPermission::PublicReadExec.is_exec_permission());
        assert!(!FSPermission::PublicWrite.is_exec_permission());
        assert!(FSPermission::PublicWriteExec.is_exec_permission());
    }

    #[cfg(unix)]
    #[test]
    fn directory_structure_matches_go_example() {
        let base = scratch_dir("example");
        let root = DirStructure::new(&base, FSPermission::PublicReadExec);
        let secret = root.child_dir("secret", FSPermission::AdminOnlyExec);
        let repo = root.child_dir("repo", FSPermission::PublicWriteExec);
        let _a = repo.child_dir("a", FSPermission::AdminOnlyExec);
        let b = repo.child_dir("b", FSPermission::PublicReadExec);
        let c = b.child_dir("c", FSPermission::PublicWriteExec);

        root.ensure().unwrap();
        c.ensure().unwrap();
        secret.ensure().unwrap();
        b.ensure_rel_dir(&["d", "e"]).unwrap();
        b.ensure_rel_path("d/f/g/h").unwrap();

        assert_eq!(mode_of(&base), 0o755);
        assert_eq!(mode_of(&base.join("secret")), 0o700);
        assert_eq!(mode_of(&base.join("repo")), 0o777);
        // `a` is only defined, never ensured, so it is not created.
        assert!(!base.join("repo/a").exists());
        assert_eq!(mode_of(&base.join("repo/b")), 0o755);
        assert_eq!(mode_of(&base.join("repo/b/c")), 0o777);
        assert_eq!(mode_of(&base.join("repo/b/d")), 0o755);
        assert_eq!(mode_of(&base.join("repo/b/d/e")), 0o755);
        assert_eq!(mode_of(&base.join("repo/b/d/f")), 0o755);
        assert_eq!(mode_of(&base.join("repo/b/d/f/g")), 0o755);
        assert_eq!(mode_of(&base.join("repo/b/d/f/g/h")), 0o755);

        let _ = std::fs::remove_dir_all(&base);
    }

    #[test]
    fn child_dir_updates_existing_permissions() {
        let base = scratch_dir("update");
        let root = DirStructure::new(&base, FSPermission::PublicReadExec);
        let first = root.child_dir("child", FSPermission::PublicRead);
        let second = root.child_dir("child", FSPermission::AdminOnlyExec);
        assert_eq!(first.permission(), FSPermission::AdminOnlyExec);
        assert_eq!(second.permission(), FSPermission::AdminOnlyExec);
        assert_eq!(Arc::strong_count(&first), 3);
        assert!(Arc::ptr_eq(&first, &second));
    }

    #[test]
    fn ensure_abs_path_rejects_out_of_scope_paths() {
        let base = scratch_dir("scope");
        let root = DirStructure::new(&base, FSPermission::PublicReadExec);
        let outside = base.parent().unwrap().join("pm-utils-elsewhere");
        let error = root.ensure_abs_path(&outside).unwrap_err();
        assert_eq!(error.kind(), ErrorKind::InvalidInput);
        assert!(error.to_string().contains("outside of DirStructure scope"));
    }

    #[test]
    fn parent_links_walk_back_to_the_root() {
        let base = scratch_dir("parent");
        let root = DirStructure::new(&base, FSPermission::PublicReadExec);
        let child = root.child_dir("child", FSPermission::PublicRead);
        let grandchild = child.child_dir("grandchild", FSPermission::PublicRead);
        assert!(root.parent().is_none());
        assert!(Arc::ptr_eq(&child.parent().unwrap(), &root));
        assert!(Arc::ptr_eq(&grandchild.parent().unwrap(), &child));
        assert_eq!(grandchild.path(), base.join("child/grandchild"));
    }
}
