use cached::proc_macro::once;
use dataurl::DataUrl;
use gdk_pixbuf::{Pixbuf, PixbufError};
use gtk_sys::{
    gtk_icon_info_free, gtk_icon_info_get_filename, gtk_icon_theme_get_default,
    gtk_icon_theme_lookup_icon, GtkIconTheme,
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

use dirs;
use ini::{Ini, ParseOption};

static mut GTK_DEFAULT_THEME: Option<*mut GtkIconTheme> = None;

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
}

impl std::fmt::Display for ProcessInfo {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(
            f,
            "{} (cmdline={}) (pid={}) (matching_path={})",
            self.exec_path, self.cmdline, self.pid, self.matching_path
        )
    }
}

pub fn get_app_info(process_info: ProcessInfo) -> Result<AppInfo> {
    {
        let cache = APP_INFO_CACHE.read().unwrap();

        if let Some(value) = cache.get(process_info.exec_path.as_str()) {
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

    if matches.is_empty() {
        APP_INFO_CACHE
            .write()
            .unwrap()
            .insert(process_info.exec_path, None);

        Err(Error::new(ErrorKind::NotFound, format!("failed to find app info")).into())
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
                        .insert(process_info.exec_path, Some(info.0.clone()));

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

        Err(Error::new(ErrorKind::NotFound, format!("failed to find app info")).into())
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
        Some(paths) => env::split_paths(&paths).map(PathBuf::from).collect(),
        None => {
            // Fallback if XDG_DATA_DIRS is not set. If it's set, it normally already contains /usr/share and
            // /usr/local/share
            vec![
                PathBuf::from("/usr/share"),
                PathBuf::from("/usr/local/share"),
            ]
        }
    };

    let mut app_dirs = Vec::new();
    for extra_dir in extra_application_dirs {
        app_dirs.push(extra_dir.join("applications"));
    }

    app_dirs.push(xdg_home.join("applications"));

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

fn try_get_app_info(
    needle: &str,
    check: CheckType,
    desktop_files: &Vec<fs::DirEntry>,
) -> Result<Vec<(AppInfo, usize)>> {
    let path = PathBuf::from(needle);

    let file_name = path.as_path().file_name().unwrap_or_default().to_str();

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

                if exec.to_lowercase().contains(needle) {
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

    if result.len() > 0 {
        Ok(result)
    } else {
        Err(Error::new(ErrorKind::NotFound, "no matching .desktop files found").into())
    }
}

fn parse_app_info(props: &ini::Properties) -> AppInfo {
    AppInfo {
        icon_dataurl: "".to_string(),
        app_name: props.get("Name").unwrap_or_default().to_string(),
        comment: props.get("Comment").unwrap_or_default().to_string(),
        icon_name: props.get("Icon").unwrap_or_default().to_string(),
    }
}

fn get_icon_as_png_dataurl(name: &str, size: i8) -> Result<(String, String)> {
    unsafe {
        if GTK_DEFAULT_THEME.is_none() {
            let theme = gtk_icon_theme_get_default();
            if theme.is_null() {
                debug!("You have to initialize GTK!");
                return Err(Error::new(ErrorKind::Other, "You have to initialize GTK!").into());
            }

            let theme = gtk_icon_theme_get_default();
            GTK_DEFAULT_THEME = Some(theme);
        }
    }

    let mut icons = Vec::new();

    // push the name
    icons.push(name);

    // if we don't find the icon by it's name and it includes an extension,
    // drop the extension and try without.
    let name_without_ext;
    if let Some(ext) = PathBuf::from(name).extension() {
        let ext = ext.to_str().unwrap();

        let mut ext_dot = String::from(".").to_owned();
        ext_dot.push_str(ext);

        name_without_ext = name.replace(ext_dot.as_str(), "");
        icons.push(name_without_ext.as_str());
    } else {
        name_without_ext = String::from(name);
    }

    // The xdg-desktop icon specification allows a fallback for icons that contains dashes.
    // i.e. the following lookup order is used:
    //      - network-wired-secure
    //      - network-wired
    //      - network
    //
    name_without_ext
        .split("-")
        .for_each(|part| icons.push(part));

    for name in icons {
        debug!("trying to load icon {}", name);

        unsafe {
            let c_str = CString::new(name).unwrap();

            let icon_info = gtk_icon_theme_lookup_icon(
                GTK_DEFAULT_THEME.unwrap(),
                c_str.as_ptr() as *const c_char,
                size as c_int,
                0,
            );
            if icon_info.is_null() {
                dbg!("failed to lookup icon {}", name);

                continue;
            }

            let filename = gtk_icon_info_get_filename(icon_info);

            let filename = CStr::from_ptr(filename).to_str().unwrap().to_string();

            gtk_icon_info_free(icon_info);

            match read_and_convert_pixbuf(filename.clone()) {
                Ok(pb) => return Ok((filename, pb)),
                Err(err) => {
                    dbg!("failed to load icon from {}: {}", filename, err.to_string());

                    continue;
                }
            }
        }
    }

    Err(Error::new(ErrorKind::NotFound, "failed to find icon").into())
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
            error!("failed to load icon pixbuf: {}", err.to_string());

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
            Err(err) => {
                return Err(glib::Error::new(
                    PixbufError::Failed,
                    err.to_string().as_str(),
                ));
            }
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
                    })
                    .expect(
                        format!(
                            "expected to find app info for {} ({})",
                            bin,
                            cmd.to_string()
                        )
                        .as_str(),
                    );

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
