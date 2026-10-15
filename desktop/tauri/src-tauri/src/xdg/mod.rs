use cached::proc_macro::once;
use dataurl::DataUrl;
use gdk_pixbuf::{Pixbuf, PixbufError};
use gtk_sys::{
    gtk_icon_info_free, gtk_icon_info_get_filename, gtk_icon_theme_get_default,
    gtk_icon_theme_lookup_icon,
};
use log::{debug, error};
use std::collections::HashMap;
use std::ffi::{c_char, c_int};
use std::ffi::{CStr, CString};
use std::io;
use std::path::{Path, PathBuf};
use std::sync::{Arc, RwLock};
use std::{
    env, fs,
    io::{Error, ErrorKind},
};
use thiserror::Error;

use ini::{Ini, ParseOption};

lazy_static! {
    static ref APP_INFO_CACHE: Arc<RwLock<HashMap<String, Option<AppInfo>>>> =
        Arc::new(RwLock::new(HashMap::new()));
}

#[derive(Debug, Error)]
pub enum LookupError {
    #[error(transparent)]
    IoError(#[from] std::io::Error),
}

pub type Result<T> = std::result::Result<T, LookupError>;

#[derive(Clone, serde::Serialize)]
pub struct AppInfo {
    pub icon_name: String,
    pub app_name: String,
    pub icon_dataurl: String,
    pub comment: String,
}

impl Default for AppInfo {
    fn default() -> Self {
        AppInfo {
            icon_dataurl: "".to_string(),
            icon_name: "".to_string(),
            app_name: "".to_string(),
            comment: "".to_string(),
        }
    }
}

#[derive(Clone, serde::Serialize, Debug)]
pub struct ProcessInfo {
    pub exec_path: String,
    pub cmdline: String,
    pub pid: i64,
    pub matching_path: String,
    /// Desktop entry ID of the application the process belongs to, when the
    /// caller knows it for certain (a flatpak's application ID). Looked up
    /// exactly as `<id>.desktop` before any path or name matching.
    pub desktop_id: Option<String>,
    /// A directory the process's binary lives under. When set, only desktop
    /// entries whose Exec program is an absolute path under this directory
    /// are considered, and only if they all name the same icon; path and
    /// name matching is skipped.
    pub app_dir: Option<String>,
}

impl std::fmt::Display for ProcessInfo {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(
            f,
            "{} (cmdline={}) (pid={}) (matching_path={}) (desktop_id={}) (app_dir={})",
            self.exec_path,
            self.cmdline,
            self.pid,
            self.matching_path,
            self.desktop_id.as_deref().unwrap_or(""),
            self.app_dir.as_deref().unwrap_or("")
        )
    }
}

/// Key of the APP_INFO_CACHE entry for a lookup. It covers every input the
/// lookup uses (exec_path, cmdline, matching_path), so that two processes
/// sharing an executable but differing in command line or matching path (an
/// interpreter running different scripts, an AppImage and its mounted
/// binary) do not share a cached result. The pid is not part of a lookup.
fn cache_key(process_info: &ProcessInfo) -> String {
    format!(
        "{}\0{}\0{}\0{}\0{}",
        process_info.exec_path,
        process_info.cmdline,
        process_info.matching_path,
        process_info.desktop_id.as_deref().unwrap_or(""),
        process_info.app_dir.as_deref().unwrap_or("")
    )
}

/// Finds the desktop entry and icon of a process. An exact desktop ID is tried first; when it
/// yields no entry, the directory hint is used if given, otherwise the process's paths and name
/// are matched against the installed desktop entries. Successful results and conclusive misses
/// are cached by all lookup inputs; failures are not.
pub fn get_app_info(process_info: ProcessInfo) -> Result<AppInfo> {
    let key = cache_key(&process_info);
    {
        let cache = APP_INFO_CACHE.read().unwrap();

        if let Some(value) = cache.get(key.as_str()) {
            match value {
                Some(app_info) => return Ok(app_info.clone()),
                None => {
                    return Err(LookupError::IoError(io::Error::new(
                        io::ErrorKind::NotFound,
                        "not found",
                    )))
                }
            }
        }
    }

    let mut needles = Vec::new();
    if !process_info.exec_path.is_empty() {
        needles.push(process_info.exec_path.as_str())
    }
    if !process_info.cmdline.is_empty() {
        needles.push(process_info.cmdline.as_str())
    }
    if !process_info.matching_path.is_empty() {
        needles.push(process_info.matching_path.as_str())
    }

    // sort and deduplicate
    needles.sort();
    needles.dedup();

    debug!("Searching app info for {:?}", process_info);

    let mut desktop_files = Vec::new();
    for dir in get_application_directories()? {
        let mut files = find_desktop_files(dir.as_path())?;
        desktop_files.append(&mut files);
    }

    let mut matches = Vec::new();

    // An exact desktop entry ID (a flatpak's application ID) identifies the
    // application without guessing: use `<id>.desktop` when it exists, and
    // only fall back to path and name matching when it does not.
    if let Some(id) = process_info
        .desktop_id
        .as_deref()
        .filter(|id| !id.is_empty())
    {
        if let Some(info) = exact_desktop_entry(id, &desktop_files)? {
            debug!(
                "[xdg] exact desktop entry {}.desktop for {:?}",
                id, process_info
            );
            matches.push((info, 0));
        }
    }

    if matches.is_empty() {
        // A directory the binary lives under: the entries whose program is
        // installed under it, if they agree on one icon. Nothing else is tried
        // in that case; the caller uses this as a last, display-only resort.
        if let Some(dir) = process_info
            .app_dir
            .as_deref()
            .filter(|dir| !dir.is_empty())
        {
            if let Some(info) = desktop_entry_under(dir, &desktop_files)? {
                debug!("[xdg] desktop entry under {} for {:?}", dir, process_info);
                matches.push((info, 0));
            }
        } else {
            for needle in needles.clone() {
                debug!("Trying needle {} on exec path", needle);

                match try_get_app_info(needle, CheckType::Exec, &desktop_files) {
                    Ok(mut result) => {
                        matches.append(&mut result);
                    }
                    Err(LookupError::IoError(ioerr)) => {
                        if ioerr.kind() != ErrorKind::NotFound {
                            return Err(ioerr.into());
                        }
                    }
                };

                match try_get_app_info(needle, CheckType::Name, &desktop_files) {
                    Ok(mut result) => {
                        matches.append(&mut result);
                    }
                    Err(LookupError::IoError(ioerr)) => {
                        if ioerr.kind() != ErrorKind::NotFound {
                            return Err(ioerr.into());
                        }
                    }
                };
            }
        }
    }

    if matches.is_empty() {
        APP_INFO_CACHE.write().unwrap().insert(key, None);

        Err(Error::new(ErrorKind::NotFound, "failed to find app info".to_string()).into())
    } else {
        // sort matches by length
        matches.sort_by(|a, b| a.1.cmp(&b.1));

        for mut info in matches {
            match get_icon_as_png_dataurl(&info.0.icon_name, 32) {
                Ok(du) => {
                    debug!(
                        "[xdg] best match for {:?} is {:?} with len {}",
                        process_info, info.0.icon_name, info.1
                    );

                    info.0.icon_dataurl = du.1;

                    APP_INFO_CACHE
                        .write()
                        .unwrap()
                        .insert(key, Some(info.0.clone()));

                    return Ok(info.0);
                }
                Err(err) => {
                    dbg!(
                        "{}: failed to get icon: {}",
                        info.0.icon_name,
                        err.to_string()
                    );
                }
            };
        }

        // A desktop entry was found, so this is not a miss: the application
        // is known, only its icon could not be loaded. Callers must not treat
        // it as "no icon exists" (and it is not cached as such).
        Err(Error::new(
            ErrorKind::Other,
            "desktop entry found but its icon could not be loaded".to_string(),
        )
        .into())
    }
}

/// Returns a vector of application directories that are expected
/// to contain all .desktop files the current user has access to.
/// The result of this function is cached for 5 minutes as it's not expected
/// that application directories actually change.
#[once(time = 300, sync_writes = true, result = true)]
fn get_application_directories() -> Result<Vec<PathBuf>> {
    let xdg_home = match env::var_os("XDG_DATA_HOME") {
        Some(path) => PathBuf::from(path),
        None => {
            let home = dirs::home_dir()
                .ok_or(Error::new(ErrorKind::Other, "Failed to get home directory"))?;

            home.join(".local/share")
        }
    };

    let extra_application_dirs = match env::var_os("XDG_DATA_DIRS") {
        Some(paths) => env::split_paths(&paths).collect(),
        None => {
            // Fallback if XDG_DATA_DIRS is not set. If it's set, it normally already contains /usr/share and
            // /usr/local/share
            vec![
                PathBuf::from("/usr/share"),
                PathBuf::from("/usr/local/share"),
            ]
        }
    };

    // Search the user's application directory before the system ones, as
    // the XDG base directory specification orders them. The exact desktop-ID
    // lookup therefore selects the user's copy of an entry first. (The
    // directory scan inspects every entry and does not deduplicate names.)
    let mut app_dirs = vec![xdg_home.join("applications")];
    for extra_dir in extra_application_dirs {
        app_dirs.push(extra_dir.join("applications"));
    }

    Ok(app_dirs)
}

// TODO(ppacher): cache the result of find_desktop_files as well.
// Though, seems like we cannot use the #[cached::proc_macro::cached] or #[cached::proc_macro::once] macros here
// because [`Result<Vec<fs::DirEntry>>>`] does not implement [`Clone`]
fn find_desktop_files(path: &Path) -> Result<Vec<fs::DirEntry>> {
    match path.read_dir() {
        Ok(files) => {
            let desktop_files = files
                .filter_map(|entry| entry.ok())
                .filter(|entry| match entry.file_type() {
                    Ok(ft) => ft.is_file() || ft.is_symlink(),
                    _ => false,
                })
                .filter(|entry| entry.file_name().to_string_lossy().ends_with(".desktop"))
                .collect::<Vec<_>>();

            Ok(desktop_files)
        }
        Err(err) => {
            // We ignore NotFound errors here because not all application
            // directories need to exist.
            if err.kind() == ErrorKind::NotFound {
                Ok(Vec::new())
            } else {
                Err(err.into())
            }
        }
    }
}

enum CheckType {
    Name,
    Exec,
}

/// The program of an Exec= value: its first argument, for the subset of the
/// desktop entry specification's quoting that program paths use: either a
/// double-quoted string, in which only `\\"` and `\\\\` are accepted as
/// escapes, or the run of characters up to the first whitespace. The value
/// is taken raw (the reader's own unescaping is off), so the specification's
/// two escaping layers are not implemented; anything outside this subset
/// (an unterminated quote, another backslash escape, an empty value) yields
/// "", which never matches a directory. Field codes and further arguments
/// are never part of the result.
fn exec_program(exec: &str) -> String {
    let exec = exec.trim_start();
    if let Some(rest) = exec.strip_prefix('"') {
        let mut out = String::new();
        let mut chars = rest.chars();
        while let Some(c) = chars.next() {
            match c {
                '\\' => match chars.next() {
                    Some(escaped @ ('"' | '\\')) => out.push(escaped),
                    _ => return String::new(),
                },
                '"' => return out,
                c => out.push(c),
            }
        }
        return String::new();
    }
    exec.split_whitespace().next().unwrap_or("").to_string()
}

/// True for an absolute path whose components are all non-empty and neither
/// "." nor "..".
fn is_canonical_path(path: &str) -> bool {
    path.starts_with('/')
        && path[1..]
            .split('/')
            .all(|c| !c.is_empty() && c != "." && c != "..")
}

/// Returns the [Desktop Entry] whose Exec program is a canonical absolute
/// path under `dir` (a proper descendant, `dir` itself excluded). None when
/// no entry qualifies. When several qualify they must all name the same
/// Icon; otherwise this is an error ("several applications"), so that the
/// caller can tell an empty directory from an ambiguous one. This scans
/// every desktop file, so one that does not parse is skipped (with a debug
/// line) rather than failing the lookup; only the named file matters in
/// exact_desktop_entry, which is why that one reports a parse error.
fn desktop_entry_under(dir: &str, desktop_files: &[fs::DirEntry]) -> Result<Option<AppInfo>> {
    let dir = dir.trim_end_matches('/');
    if dir.is_empty() || !is_canonical_path(dir) {
        return Ok(None);
    }
    let prefix = format!("{}/", dir);
    let mut found: Option<AppInfo> = None;
    for file in desktop_files {
        // Quote handling stays off here: the reader would strip the quotes
        // around a program path with spaces and lose where it ends. The
        // Exec value is read raw and split by exec_program instead.
        let content = match Ini::load_from_file_opt(
            file.path(),
            ParseOption {
                enabled_escape: false,
                enabled_quote: false,
            },
        ) {
            Ok(content) => content,
            Err(err) => {
                debug!("[xdg] skipping {}: {}", file.path().to_string_lossy(), err);
                continue;
            }
        };
        let section = match content.section(Some("Desktop Entry")) {
            Some(section) => section,
            None => continue,
        };
        let program = exec_program(section.get("Exec").unwrap_or_default());
        if !program.starts_with(&prefix) || !is_canonical_path(&program) {
            continue;
        }
        let info = parse_app_info(section);
        match &found {
            None => found = Some(info),
            Some(first) if first.icon_name == info.icon_name => {}
            Some(_) => {
                debug!(
                    "[xdg] several applications under {}, not using the directory",
                    dir
                );
                return Err(Error::new(
                    ErrorKind::Other,
                    format!("several applications are installed under {}", dir),
                )
                .into());
            }
        }
    }
    Ok(found)
}

/// Returns the [Desktop Entry] of the desktop file named exactly `<id>.desktop`
/// among the collected desktop files, or None when there is none. The ID is
/// only compared with file names that were already enumerated from the
/// application directories; it is never joined into a path. When the same
/// file name exists in several directories, the first in the collected
/// directory order wins. A file that exists but does not parse is an error.
fn exact_desktop_entry(id: &str, desktop_files: &[fs::DirEntry]) -> Result<Option<AppInfo>> {
    let wanted = format!("{}.desktop", id);
    for file in desktop_files {
        if file.file_name().to_string_lossy() != wanted {
            continue;
        }
        let content = Ini::load_from_file_opt(
            file.path(),
            ParseOption {
                enabled_escape: false,
                enabled_quote: true,
            },
        )
        .map_err(|err| Error::new(ErrorKind::Other, err.to_string()))?;
        return Ok(content.section(Some("Desktop Entry")).map(parse_app_info));
    }
    Ok(None)
}

/// Collects desktop entries whose Exec or Name matches the needle, paired with the matched
/// length. Comparison is case-insensitive.
fn try_get_app_info(
    needle: &str,
    check: CheckType,
    desktop_files: &Vec<fs::DirEntry>,
) -> Result<Vec<(AppInfo, usize)>> {
    let path = PathBuf::from(needle);

    // Desktop entries are compared lower-cased below; the needle must be too.
    let needle = needle.to_lowercase();
    let file_name = path
        .as_path()
        .file_name()
        .unwrap_or_default()
        .to_str()
        .map(|name| name.to_lowercase());
    let file_name = file_name.as_deref();

    let mut result = Vec::new();

    for file in desktop_files {
        let content = Ini::load_from_file_opt(
            file.path(),
            ParseOption {
                enabled_escape: false,
                enabled_quote: true,
            },
        )
        .map_err(|err| Error::new(ErrorKind::Other, err.to_string()))?;

        let desktop_section = match content.section(Some("Desktop Entry")) {
            Some(section) => section,
            None => {
                continue;
            }
        };

        let matches = match check {
            CheckType::Name => {
                let name = match desktop_section.get("Name") {
                    Some(name) => name,
                    None => {
                        continue;
                    }
                };

                if let Some(file_name) = file_name {
                    if name.to_lowercase().contains(file_name) {
                        file_name.len()
                    } else {
                        0
                    }
                } else {
                    0
                }
            }
            CheckType::Exec => {
                let exec = match desktop_section.get("Exec") {
                    Some(exec) => exec,
                    None => {
                        continue;
                    }
                };

                if exec.to_lowercase().contains(needle.as_str()) {
                    needle.len()
                } else if let Some(file_name) = file_name {
                    if exec.to_lowercase().starts_with(file_name) {
                        file_name.len()
                    } else {
                        0
                    }
                } else {
                    0
                }
            }
        };

        if matches > 0 {
            debug!(
                "[xdg] found matching desktop for needle {} file at {}",
                needle,
                file.path().to_string_lossy()
            );

            let info = parse_app_info(desktop_section);

            result.push((info, matches));
        }
    }

    if !result.is_empty() {
        Ok(result)
    } else {
        Err(Error::new(ErrorKind::NotFound, "no matching .desktop files found").into())
    }
}

/// The directories that hold installed icon themes, in lookup order:
/// $XDG_DATA_HOME/icons (default ~/.local/share/icons), ~/.icons, and
/// <dir>/icons for each entry of $XDG_DATA_DIRS (default /usr/share and
/// /usr/local/share), plus /usr/share/pixmaps as a flat directory.
fn icon_base_directories() -> Vec<PathBuf> {
    let mut dirs = Vec::new();
    if let Some(home) = dirs::home_dir() {
        match env::var_os("XDG_DATA_HOME") {
            Some(path) => dirs.push(PathBuf::from(path).join("icons")),
            None => dirs.push(home.join(".local/share/icons")),
        }
        dirs.push(home.join(".icons"));
    }
    match env::var_os("XDG_DATA_DIRS") {
        Some(paths) => {
            for p in env::split_paths(&paths) {
                dirs.push(p.join("icons"));
            }
        }
        None => {
            dirs.push(PathBuf::from("/usr/share/icons"));
            dirs.push(PathBuf::from("/usr/local/share/icons"));
        }
    }
    dirs.push(PathBuf::from("/usr/share/pixmaps"));
    dirs
}

/// The pixel size an icon theme size directory stands for ("48x48", "48",
/// "symbolic" and the like); scalable directories rank above every size.
fn icon_dir_size(dir_name: &str) -> u32 {
    if dir_name == "scalable" {
        return u32::MAX;
    }
    dir_name
        .split('x')
        .next()
        .and_then(|s| s.split('@').next())
        .and_then(|s| s.parse::<u32>().ok())
        .unwrap_or(0)
}

/// Looks for `<name>.svg|png|xpm` in every installed icon theme under the
/// given base directories (theme/<size>/<context>/<name>.<ext>, the two
/// level layout of the icon theme specification, plus the flat layout of
/// pixmaps). Among the candidates the largest size wins, scalable first;
/// themes are visited in base-directory order and the first theme that has
/// the icon decides. The search is bounded to those three levels: no
/// recursion into anything deeper.
fn find_icon_in_installed_themes(name: &str, bases: &[PathBuf]) -> Option<PathBuf> {
    if name.is_empty() || name.contains('/') {
        return None;
    }
    let exts = ["svg", "png", "xpm"];
    let file_names: Vec<String> = exts.iter().map(|e| format!("{}.{}", name, e)).collect();

    let in_dir = |dir: &Path| -> Option<PathBuf> {
        file_names.iter().map(|f| dir.join(f)).find(|p| p.is_file())
    };

    for base in bases {
        // Flat layout (pixmaps).
        if let Some(p) = in_dir(base) {
            return Some(p);
        }
        let themes = match base.read_dir() {
            Ok(entries) => entries,
            Err(_) => continue,
        };
        let mut theme_dirs: Vec<PathBuf> = themes
            .filter_map(|e| e.ok())
            .map(|e| e.path())
            .filter(|p| p.is_dir())
            .collect();
        theme_dirs.sort();
        for theme in theme_dirs {
            let mut best: Option<(u32, PathBuf)> = None;
            let sizes = match theme.read_dir() {
                Ok(entries) => entries,
                Err(_) => continue,
            };
            for size in sizes
                .filter_map(|e| e.ok())
                .map(|e| e.path())
                .filter(|p| p.is_dir())
            {
                let rank = icon_dir_size(&size.file_name().unwrap_or_default().to_string_lossy());
                let contexts = match size.read_dir() {
                    Ok(entries) => entries,
                    Err(_) => continue,
                };
                for context in contexts
                    .filter_map(|e| e.ok())
                    .map(|e| e.path())
                    .filter(|p| p.is_dir())
                {
                    if let Some(p) = in_dir(&context) {
                        if best.as_ref().map_or(true, |(r, _)| rank > *r) {
                            best = Some((rank, p));
                        }
                    }
                }
            }
            if let Some((_, p)) = best {
                return Some(p);
            }
        }
    }
    None
}

fn parse_app_info(props: &ini::Properties) -> AppInfo {
    AppInfo {
        icon_dataurl: "".to_string(),
        app_name: props.get("Name").unwrap_or_default().to_string(),
        comment: props.get("Comment").unwrap_or_default().to_string(),
        icon_name: props.get("Icon").unwrap_or_default().to_string(),
    }
}

/// Loads the icon a desktop entry names as a PNG data URL (see `get_icon_as_png_dataurl_with_bases`).
fn get_icon_as_png_dataurl(name: &str, size: i8) -> Result<(String, String)> {
    get_icon_as_png_dataurl_with_bases(name, size, &icon_base_directories())
}

/// Loads the icon a desktop entry names, in three stages:
///   1. an absolute path is loaded as that file and nothing else (desktop
///      entry specification); a themed name is looked up in the active
///      theme and its inheritance chain, as given and with its extension
///      stripped;
///   2. the same exact names are looked for once in the other installed
///      icon themes under `bases` (a desktop entry commonly names an icon
///      that an icon pack provides; without the pack active that icon was
///      unreachable);
///   3. the existing component-splitting fallback in the active theme: each
///      dash-separated component of the name is tried on its own
///      ("app-browser" → "app", "browser"), which degrades to a less
///      specific icon and therefore comes last. It never applies to a path.
fn get_icon_as_png_dataurl_with_bases(
    name: &str,
    size: i8,
    bases: &[PathBuf],
) -> Result<(String, String)> {
    if name.starts_with('/') {
        if !Path::new(name).is_file() {
            return Err(Error::new(
                ErrorKind::Other,
                format!("icon file {} does not exist", name),
            )
            .into());
        }
        return match read_and_convert_pixbuf(name.to_string()) {
            Ok(pb) => Ok((name.to_string(), pb)),
            Err(err) => Err(Error::new(
                ErrorKind::Other,
                format!("failed to load icon file {}: {}", name, err),
            )
            .into()),
        };
    }

    // gtk_icon_theme_get_default() is lightweight - it returns a borrowed reference to GTK's singleton icon theme
    let theme = unsafe { gtk_icon_theme_get_default() };
    if theme.is_null() {
        return Err(Error::new(ErrorKind::Other, "GTK not initialized").into());
    }

    // The exact names: as given and, if it carries an extension, without it.
    let mut exact_names: Vec<String> = vec![name.to_string()];
    let name_without_ext = match PathBuf::from(name).extension().and_then(|e| e.to_str()) {
        Some(ext) => {
            let without = name.replace(&format!(".{}", ext), "");
            exact_names.push(without.clone());
            without
        }
        None => name.to_string(),
    };

    // Stage 1: the active theme.
    for n in &exact_names {
        if let Some(result) = lookup_in_active_theme(theme, n, size) {
            return Ok(result);
        }
    }

    // Stage 2: the other installed themes, once.
    for n in &exact_names {
        if let Some(file) = find_icon_in_installed_themes(n, bases) {
            debug!(
                "found icon {} in another installed theme: {}",
                n,
                file.display()
            );
            let filename = file.to_string_lossy().to_string();
            match read_and_convert_pixbuf(filename.clone()) {
                Ok(pb) => return Ok((filename, pb)),
                Err(err) => {
                    dbg!("failed to load icon from {}: {}", filename, err.to_string());
                }
            }
        }
    }

    // Stage 3: dash-split fallbacks in the active theme.
    for n in name_without_ext.split('-') {
        if exact_names.iter().any(|e| e == n) {
            continue;
        }
        if let Some(result) = lookup_in_active_theme(theme, n, size) {
            return Ok(result);
        }
    }

    Err(Error::new(ErrorKind::NotFound, "failed to find icon").into())
}

/// One themed lookup in the active GTK icon theme, converted to a PNG data
/// URL. None when the theme has no such icon or it cannot be loaded.
fn lookup_in_active_theme(
    theme: *mut gtk_sys::GtkIconTheme,
    name: &str,
    size: i8,
) -> Option<(String, String)> {
    debug!("trying to load icon {}", name);

    unsafe {
        let c_str = CString::new(name).ok()?;

        let icon_info =
            gtk_icon_theme_lookup_icon(theme, c_str.as_ptr() as *const c_char, size as c_int, 0);
        if icon_info.is_null() {
            dbg!("failed to lookup icon {}", name);
            return None;
        }

        // NULL for an icon without a backing file (built-in or resource
        // icons): nothing to load, and never a pointer to dereference.
        let filename = gtk_icon_info_get_filename(icon_info);
        if filename.is_null() {
            gtk_icon_info_free(icon_info);
            return None;
        }
        let filename = CStr::from_ptr(filename).to_str().map(|s| s.to_string());
        gtk_icon_info_free(icon_info);
        let filename = filename.ok()?;

        match read_and_convert_pixbuf(filename.clone()) {
            Ok(pb) => Some((filename, pb)),
            Err(err) => {
                dbg!("failed to load icon from {}: {}", filename, err.to_string());
                None
            }
        }
    }
}

/*
fn get_icon_as_file_2(ext: &str, size: i32) -> io::Result<(String, Vec<u8>)> {
    let result: String;
    let buf: Vec<u8>;

    unsafe {
        let filename = CString::new(ext).unwrap();
        let null: u8 = 0;
        let p_null = &null as *const u8;
        let nullsize: usize = 0;
        let mut res = 0;
        let p_res = &mut res as *mut i32;
        let p_res = gio_sys::g_content_type_guess(filename.as_ptr(), p_null, nullsize, p_res);
        let icon = gio_sys::g_content_type_get_icon(p_res);
        g_free(p_res as *mut c_void);
        if DEFAULT_THEME.is_none() {
            let theme = gtk_icon_theme_get_default();
            if theme.is_null() {
                println!("You have to initialize GTK!");
                return Err(io::Error::new(io::ErrorKind::Other, "You have to initialize GTK!"))
            }
            let theme = gtk_icon_theme_get_default();
            DEFAULT_THEME = Some(theme);
        }
        let icon_names = gio_sys::g_themed_icon_get_names(icon as *mut GThemedIcon) as *mut *const i8;
        let icon_info = gtk_icon_theme_choose_icon(DEFAULT_THEME.unwrap(), icon_names, size, GTK_ICON_LOOKUP_NO_SVG);
        let filename = gtk_icon_info_get_filename(icon_info);

        gtk_icon_info_free(icon_info);

        result = CStr::from_ptr(filename).to_str().unwrap().to_string();

        buf = match read_and_convert_pixbuf(result.clone()) {
            Ok(pb) => pb,
            Err(_) => Vec::new(),
        };

        g_object_unref(icon as *mut GObject);
    }

    Ok((result, buf))

}
*/

fn read_and_convert_pixbuf(result: String) -> std::result::Result<String, glib::Error> {
    let pixbuf = match Pixbuf::from_file(result.clone()) {
        Ok(data) => Ok(data),
        Err(err) => {
            error!("failed to load icon pixbuf: {}", err);

            Pixbuf::from_resource(result.clone().as_str())
        }
    };

    match pixbuf {
        Ok(data) => match data.save_to_bufferv("png", &[]) {
            Ok(data) => {
                let mut du = DataUrl::new();

                du.set_media_type(Some("image/png".to_string()));
                du.set_data(&data);

                Ok(du.to_string())
            }
            Err(err) => Err(glib::Error::new(
                PixbufError::Failed,
                err.to_string().as_str(),
            )),
        },
        Err(err) => Err(err),
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use ctor::ctor;
    use log::warn;
    use which::which;

    // Use the ctor create to setup a global initializer before our tests are executed.
    #[ctor]
    fn init() {
        // we need to initialize GTK before running our tests.
        // This is only required when unit tests are executed as
        // GTK will otherwise be initialize by Tauri.

        gtk::init().expect("failed to initialize GTK for tests")
    }

    /// The cache key separates lookups that differ in any input and ignores the pid.
    #[test]
    fn test_cache_key_covers_all_lookup_inputs() {
        let base = ProcessInfo {
            exec_path: "/usr/bin/python3".to_string(),
            cmdline: "python3 /opt/a/a.py".to_string(),
            matching_path: "/usr/bin/python3".to_string(),
            pid: 1,
            desktop_id: None,
            app_dir: None,
        };
        let other_app_dir = ProcessInfo {
            app_dir: Some("/opt/example".to_string()),
            ..base.clone()
        };
        let other_desktop_id = ProcessInfo {
            desktop_id: Some("org.example.App".to_string()),
            ..base.clone()
        };
        let same_lookup = ProcessInfo {
            pid: 2,
            ..base.clone()
        };
        let other_exec = ProcessInfo {
            exec_path: "/usr/bin/python3.12".to_string(),
            ..base.clone()
        };
        let other_cmdline = ProcessInfo {
            cmdline: "python3 /opt/b/b.py".to_string(),
            ..base.clone()
        };
        let other_matching = ProcessInfo {
            matching_path: "/opt/a/a.AppImage".to_string(),
            ..base.clone()
        };

        assert_eq!(cache_key(&base), cache_key(&same_lookup));
        assert_ne!(cache_key(&base), cache_key(&other_cmdline));
        assert_ne!(cache_key(&base), cache_key(&other_matching));
        assert_ne!(cache_key(&base), cache_key(&other_exec));
        assert_ne!(cache_key(&base), cache_key(&other_desktop_id));
        assert_ne!(cache_key(&base), cache_key(&other_app_dir));
    }

    /// Directory matching: agreeing entries, exclusions, ambiguity, quoting and canonical paths.
    #[test]
    fn test_desktop_entry_under() {
        let dir = write_desktop_dir(
            "under",
            &[
                (
                    "ladybird.desktop",
                    "[Desktop Entry]\nName=Ladybird\nIcon=/opt/ladybird/icon.png\nExec=/opt/ladybird/release/bin/Ladybird %u\n",
                ),
                (
                    "ladybird-quoted.desktop",
                    "[Desktop Entry]\nName=Ladybird (quoted)\nIcon=/opt/ladybird/icon.png\nExec=\"/opt/ladybird/release/bin/Ladybird\" --new-window %u\n",
                ),
                (
                    "vendor-a.desktop",
                    "[Desktop Entry]\nName=A\nIcon=a\nExec=/opt/vendor/a/bin/a\n",
                ),
                (
                    "vendor-b.desktop",
                    "[Desktop Entry]\nName=B\nIcon=b\nExec=/opt/vendor/b/bin/b\n",
                ),
                (
                    "spaces.desktop",
                    "[Desktop Entry]\nName=Spaces\nIcon=spaces\nExec=\"/opt/with space/bin/app\" --flag %u\n",
                ),
                (
                    "dotdot.desktop",
                    "[Desktop Entry]\nName=DotDot\nIcon=dotdot\nExec=/opt/escape/../ladybird/release/bin/x\n",
                ),
                (
                    "unterminated.desktop",
                    "[Desktop Entry]\nName=Unterminated\nIcon=unterminated\nExec=\"/opt/unterminated/bin/app --x\n",
                ),
            ],
        );
        let files = find_desktop_files(&dir).unwrap();

        // Entries under the directory that agree on the icon.
        let info = desktop_entry_under("/opt/ladybird/release", &files)
            .unwrap()
            .unwrap();
        assert_eq!(info.icon_name, "/opt/ladybird/icon.png");
        let info = desktop_entry_under("/opt/ladybird/release/", &files)
            .unwrap()
            .unwrap();
        assert_eq!(info.icon_name, "/opt/ladybird/icon.png");

        // The directory itself is not "under" it; a sibling tree is not either.
        assert!(
            desktop_entry_under("/opt/ladybird/release/bin/Ladybird", &files)
                .unwrap()
                .is_none()
        );
        assert!(desktop_entry_under("/opt/ladybirds", &files)
            .unwrap()
            .is_none());

        // Two applications under one directory: an error, distinct from "none".
        assert!(desktop_entry_under("/opt/vendor", &files).is_err());
        assert_eq!(
            desktop_entry_under("/opt/vendor/a", &files)
                .unwrap()
                .unwrap()
                .icon_name,
            "a"
        );

        // A quoted program with spaces, read through the INI reader.
        let info = desktop_entry_under("/opt/with space", &files)
            .unwrap()
            .unwrap();
        assert_eq!(info.icon_name, "spaces");

        // Non-canonical programs and directories never match. The file with
        // the unterminated quote does not even parse as INI and is skipped.
        assert!(desktop_entry_under("/opt/escape", &files)
            .unwrap()
            .is_none());
        assert!(desktop_entry_under("/opt/ladybird/../ladybird", &files)
            .unwrap()
            .is_none());
        assert!(desktop_entry_under("/opt/unterminated", &files)
            .unwrap()
            .is_none());
        assert!(desktop_entry_under("", &files).unwrap().is_none());

        assert_eq!(exec_program("\"/a b/c\" --x"), "/a b/c");
        assert_eq!(exec_program("\"/a \\\"q\\\" b/c\" --x"), "/a \"q\" b/c");
        assert_eq!(exec_program("\"/unterminated"), "");
        assert_eq!(exec_program("\"/a\\\\b\" x"), "/a\\b");
        assert_eq!(exec_program("\"/a\\nb\" x"), "");
        assert_eq!(exec_program("  /usr/bin/x %U"), "/usr/bin/x");
        assert_eq!(exec_program(""), "");

        let _ = fs::remove_dir_all(&dir);

        // A set with relative Exec programs only never matches any directory.
        let dir = write_desktop_dir(
            "relative-only",
            &[(
                "relative.desktop",
                "[Desktop Entry]\nName=Relative\nIcon=rel\nExec=ladybird %u\n",
            )],
        );
        let files = find_desktop_files(&dir).unwrap();
        assert!(desktop_entry_under("/", &files).unwrap().is_none());
        assert!(desktop_entry_under("/opt", &files).unwrap().is_none());
        let _ = fs::remove_dir_all(&dir);
    }

    /// An exact entry is found regardless of an unrelated malformed file, which the directory scan skips.
    #[test]
    fn test_exact_entry_wins_over_directory_and_ignores_unrelated_broken_files() {
        // The exact entry is found regardless of an unrelated malformed
        // desktop file, and the directory scan skips that file.
        let dir = write_desktop_dir(
            "both-hints",
            &[
                (
                    "org.example.App.desktop",
                    "[Desktop Entry]\nName=Example\nIcon=example\nExec=/opt/example/bin/example\n",
                ),
                ("broken.desktop", "[Desktop Entry\nName=Broken\n"),
            ],
        );
        let files = find_desktop_files(&dir).unwrap();
        let exact = exact_desktop_entry("org.example.App", &files)
            .unwrap()
            .unwrap();
        assert_eq!(exact.icon_name, "example");
        let under = desktop_entry_under("/opt/example", &files)
            .unwrap()
            .unwrap();
        assert_eq!(under.icon_name, "example");
        // The malformed file itself, asked for by name, is an error.
        assert!(exact_desktop_entry("broken", &files).is_err());
        let _ = fs::remove_dir_all(&dir);
    }

    /// A cached result, positive or negative, is served only for the same lookup inputs.
    #[test]
    fn test_cache_entries_do_not_leak_across_lookup_inputs() {
        // A cached result for one lookup must not be served for another
        // lookup that only shares the executable path: that was the v2.2.3
        // behaviour (cache keyed by exec_path alone).
        let exec_path = "/nonexistent/test-binary-for-cache-test".to_string();
        let cached_lookup = ProcessInfo {
            exec_path: exec_path.clone(),
            cmdline: "test-binary --cached".to_string(),
            matching_path: exec_path.clone(),
            pid: 1,
            desktop_id: None,
            app_dir: None,
        };
        let other_lookup = ProcessInfo {
            exec_path: exec_path.clone(),
            cmdline: "test-binary --other".to_string(),
            matching_path: "/nonexistent/other.AppImage".to_string(),
            pid: 1,
            desktop_id: None,
            app_dir: None,
        };
        let fake = AppInfo {
            app_name: "cache-test-fake".to_string(),
            ..AppInfo::default()
        };
        APP_INFO_CACHE
            .write()
            .unwrap()
            .insert(cache_key(&cached_lookup), Some(fake.clone()));

        // Same inputs, other pid: served from the cache.
        let hit = get_app_info(ProcessInfo {
            pid: 2,
            ..cached_lookup.clone()
        })
        .expect("cached entry must be served");
        assert_eq!(hit.app_name, fake.app_name);

        // Same executable, other command line and matching path: not the
        // cached entry. The real lookup for a nonexistent binary fails.
        if let Ok(info) = get_app_info(other_lookup) {
            assert_ne!(info.app_name, fake.app_name);
        }

        // A negative entry for one lookup does not block another either.
        APP_INFO_CACHE
            .write()
            .unwrap()
            .insert(cache_key(&cached_lookup), None);
        assert!(get_app_info(cached_lookup.clone()).is_err());
        let richer = ProcessInfo {
            matching_path: "/nonexistent/richer.AppImage".to_string(),
            ..cached_lookup.clone()
        };
        // Not served from the negative entry: the key differs, so a real
        // lookup runs (and fails for a nonexistent binary, as a fresh one).
        assert_ne!(cache_key(&richer), cache_key(&cached_lookup));
        let _ = get_app_info(richer);
    }

    /// Creates a temporary applications directory holding the given desktop files.
    fn write_desktop_dir(name: &str, files: &[(&str, &str)]) -> PathBuf {
        let dir = env::temp_dir().join(format!("pm-xdg-test-{}-{}", name, std::process::id()));
        let _ = fs::remove_dir_all(&dir);
        fs::create_dir_all(&dir).unwrap();
        for (file, content) in files {
            fs::write(dir.join(file), content).unwrap();
        }
        dir
    }

    /// Exact lookup by desktop ID: main entry over its action section, missing IDs, parse errors.
    #[test]
    fn test_exact_desktop_entry() {
        let dir = write_desktop_dir(
            "exact",
            &[
                (
                    "org.telegram.desktop.desktop",
                    "[Desktop Entry]\nName=Telegram\nIcon=org.telegram.desktop\nExec=/usr/bin/flatpak run org.telegram.desktop\n\n[Desktop Action Quit]\nName=Quit Telegram\nIcon=application-exit\n",
                ),
                ("broken.desktop", "[Desktop Entry\nName=Broken\n"),
                ("no-entry.desktop", "[Something Else]\nName=Other\n"),
            ],
        );
        let files = find_desktop_files(&dir).unwrap();

        // The main entry, not the action.
        let info = exact_desktop_entry("org.telegram.desktop", &files)
            .unwrap()
            .expect("entry must be found");
        assert_eq!(info.app_name, "Telegram");
        assert_eq!(info.icon_name, "org.telegram.desktop");

        // Missing: None, not an error. A traversal-looking ID cannot match either.
        assert!(exact_desktop_entry("org.example.Missing", &files)
            .unwrap()
            .is_none());
        assert!(exact_desktop_entry("../org.telegram.desktop", &files)
            .unwrap()
            .is_none());

        // Present without a [Desktop Entry] section: None.
        assert!(exact_desktop_entry("no-entry", &files).unwrap().is_none());

        // Present but malformed: an error, so the caller does not fall back silently.
        assert!(exact_desktop_entry("broken", &files).is_err());

        let _ = fs::remove_dir_all(&dir);
    }

    /// Checks exact-entry lookup and case-insensitive Exec and Name matching independently.
    #[test]
    fn test_exact_entry_precedes_needle_matching_without_blocking_it() {
        // With a desktop ID the exact entry is used; without one, every
        // needle still contributes candidates as before (the exact lookup
        // must not cut the fallback loop short).
        let dir = write_desktop_dir(
            "needles",
            &[
                (
                    "org.example.App.desktop",
                    "[Desktop Entry]\nName=Example App\nIcon=org.example.App\nExec=/usr/bin/flatpak run org.example.App\n",
                ),
                (
                    "other.desktop",
                    "[Desktop Entry]\nName=Other Tool\nIcon=other\nExec=/opt/other/bin/othertool %U\n",
                ),
            ],
        );
        let files = find_desktop_files(&dir).unwrap();

        let exact = exact_desktop_entry("org.example.App", &files)
            .unwrap()
            .unwrap();
        assert_eq!(exact.icon_name, "org.example.App");

        // Fallback matching is case-insensitive on the needle's file name.
        let by_name =
            try_get_app_info("/opt/other/bin/OtherTool", CheckType::Exec, &files).unwrap();
        assert_eq!(by_name[0].0.icon_name, "other");
        let by_name = try_get_app_info("/somewhere/Other", CheckType::Name, &files).unwrap();
        assert_eq!(by_name[0].0.icon_name, "other");

        let _ = fs::remove_dir_all(&dir);
    }

    /// An absolute Icon= path is loaded as that file; a missing file is an error, not a themed fallback.
    #[test]
    fn test_icon_file_path_is_loaded_as_a_file_without_theme_fallback() {
        // A 1x1 PNG written to a temp file: an absolute Icon= path loads
        // that file. A missing file is an error, not a fall back to a themed
        // icon derived from the file name ("app-browser.png" -> "browser").
        let dir = env::temp_dir().join(format!("pm-xdg-icon-{}", std::process::id()));
        let _ = fs::remove_dir_all(&dir);
        fs::create_dir_all(&dir).unwrap();
        let png = dir.join("app-browser.png");
        fs::write(
            &png,
            [
                0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x00, 0x00, 0x0D, 0x49, 0x48,
                0x44, 0x52, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01, 0x08, 0x06, 0x00, 0x00,
                0x00, 0x1F, 0x15, 0xC4, 0x89, 0x00, 0x00, 0x00, 0x0A, 0x49, 0x44, 0x41, 0x54, 0x78,
                0x9C, 0x63, 0x00, 0x01, 0x00, 0x00, 0x05, 0x00, 0x01, 0x0D, 0x0A, 0x2D, 0xB4, 0x00,
                0x00, 0x00, 0x00, 0x49, 0x45, 0x4E, 0x44, 0xAE, 0x42, 0x60, 0x82,
            ],
        )
        .unwrap();

        let (file, data_url) = get_icon_as_png_dataurl(png.to_str().unwrap(), 32).unwrap();
        assert_eq!(file, png.to_str().unwrap());
        assert!(
            data_url.starts_with("data:image/png"),
            "{}",
            &data_url[..40.min(data_url.len())]
        );

        let missing = dir.join("missing-app-browser.png");
        assert!(get_icon_as_png_dataurl(missing.to_str().unwrap(), 32).is_err());

        let _ = fs::remove_dir_all(&dir);
    }

    /// Installed-theme search and the staged lookup sequence on a temporary theme tree.
    #[test]
    fn test_find_icon_in_installed_themes() {
        let base = env::temp_dir().join(format!("pm-xdg-themes-{}", std::process::id()));
        let _ = fs::remove_dir_all(&base);
        let png: [u8; 67] = [
            0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x00, 0x00, 0x0D, 0x49, 0x48,
            0x44, 0x52, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01, 0x08, 0x06, 0x00, 0x00,
            0x00, 0x1F, 0x15, 0xC4, 0x89, 0x00, 0x00, 0x00, 0x0A, 0x49, 0x44, 0x41, 0x54, 0x78,
            0x9C, 0x63, 0x00, 0x01, 0x00, 0x00, 0x05, 0x00, 0x01, 0x0D, 0x0A, 0x2D, 0xB4, 0x00,
            0x00, 0x00, 0x00, 0x49, 0x45, 0x4E, 0x44, 0xAE, 0x42, 0x60, 0x82,
        ];
        let icons = base.join("icons");
        for (dir, file) in [
            ("ThemeB/16x16/apps", "pmtestthemeicon.png"),
            ("ThemeB/48x48/apps", "pmtestthemeicon.png"),
            ("ThemeB/scalable/apps", "pmtestthemeicon.svg"),
            ("ThemeA/22x22/apps", "other.png"),
            ("ThemeC/48x48/apps/deeper", "deeperonly.png"),
        ] {
            let d = icons.join(dir);
            fs::create_dir_all(&d).unwrap();
            fs::write(d.join(file), png).unwrap();
        }
        let pixmaps = base.join("pixmaps");
        fs::create_dir_all(&pixmaps).unwrap();
        fs::write(pixmaps.join("flat.png"), png).unwrap();

        let bases = vec![icons.clone(), pixmaps.clone()];
        // Scalable wins over sizes; the two-level layout only (ThemeC's deeper file is not seen).
        assert_eq!(
            find_icon_in_installed_themes("pmtestthemeicon", &bases),
            Some(icons.join("ThemeB/scalable/apps/pmtestthemeicon.svg"))
        );
        assert_eq!(
            find_icon_in_installed_themes("other", &bases),
            Some(icons.join("ThemeA/22x22/apps/other.png"))
        );
        assert_eq!(
            find_icon_in_installed_themes("flat", &bases),
            Some(pixmaps.join("flat.png"))
        );
        assert_eq!(find_icon_in_installed_themes("deeperonly", &bases), None);
        assert_eq!(find_icon_in_installed_themes("missing", &bases), None);

        // Through the staged lookup: a dashless name the active theme does
        // not have is found in the installed themes (stage 2), and a name
        // nothing has is an error. Stage 3 cannot apply to a dashless name.
        let (file, data_url) =
            get_icon_as_png_dataurl_with_bases("pmtestthemeicon", 32, &bases).unwrap();
        assert_eq!(
            file,
            icons
                .join("ThemeB/scalable/apps/pmtestthemeicon.svg")
                .to_string_lossy()
        );
        assert!(data_url.starts_with("data:image/png"));
        assert!(get_icon_as_png_dataurl_with_bases("pmtestnosuchicon", 32, &bases).is_err());
        assert_eq!(find_icon_in_installed_themes("", &bases), None);
        assert_eq!(
            find_icon_in_installed_themes("../pmtestthemeicon", &bases),
            None
        );
        assert_eq!(icon_dir_size("48x48"), 48);
        assert_eq!(icon_dir_size("48x48@2x"), 48);
        assert_eq!(icon_dir_size("scalable"), u32::MAX);
        assert_eq!(icon_dir_size("symbolic"), 0);

        let _ = fs::remove_dir_all(&base);
    }

    /// Smoke test against the host's real desktop entries; performs no lookup assertions when none
    /// of the candidate binaries exist.
    #[test]
    fn test_find_info_success() {
        // we expect at least one of the following binaries to be installed
        // on a linux system
        let test_binaries = vec![
            "vim",             // vim is mostly bundled with a .desktop file
            "blueman-manager", // blueman-manager is the default bluetooth manager on most DEs
            "nautilus",        // nautlis: file-manager on GNOME DE
            "thunar",          // thunar: file-manager on XFCE
            "dolphin",         // dolphin: file-manager on KDE
        ];

        let mut bin_found = false;

        for cmd in test_binaries {
            match which(cmd) {
                Ok(bin) => {
                    bin_found = true;

                    let bin = bin.to_string_lossy().to_string();

                    let result = get_app_info(ProcessInfo {
                        cmdline: cmd.to_string(),
                        exec_path: bin.clone(),
                        matching_path: bin.clone(),
                        pid: 0,
                        desktop_id: None,
                        app_dir: None,
                    })
                    .unwrap_or_else(|_| panic!("expected to find app info for {} ({})", bin, cmd));
                    let empty_string = String::from("");

                    // just make sure all fields are populated
                    assert_ne!(result.app_name, empty_string);
                    assert_ne!(result.comment, empty_string);
                    assert_ne!(result.icon_name, empty_string);
                    assert_ne!(result.icon_dataurl, empty_string);
                }
                Err(_) => {
                    // binary not found
                    continue;
                }
            }
        }

        if !bin_found {
            warn!("test_find_info_success: no test binary found, test was skipped")
        }
    }
}
