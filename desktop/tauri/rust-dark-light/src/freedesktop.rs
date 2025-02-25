use detect_desktop_environment::DesktopEnvironment;
use ini::Ini;
use std::path::{Path, PathBuf};
use zbus::blocking::Connection;

use crate::Mode;

const XDG_KDEGLOBALS: &str = "/etc/xdg/kdeglobals";

fn get_freedesktop_color_scheme() -> Option<Mode> {
    let conn = Connection::session();
    if conn.is_err() {
        return None;
    }
    let reply = conn.unwrap().call_method(
        Some("org.freedesktop.portal.Desktop"),
        "/org/freedesktop/portal/desktop",
        Some("org.freedesktop.portal.Settings"),
        "Read",
        &("org.freedesktop.appearance", "color-scheme"),
    );
    if let Ok(reply) = &reply {
        let theme = reply.body().deserialize::<u32>();
        if theme.is_err() {
            return None;
        }

        match theme.unwrap() {
            1 => Some(Mode::Dark),
            2 => Some(Mode::Light),
            _ => None,
        }
    } else {
        None
    }
}

fn detect_gtk(pattern: &str) -> Mode {
    match dconf_rs::get_string(pattern) {
        Ok(theme) => Mode::from(theme.to_lowercase().contains("dark")),
        Err(_) => Mode::Light,
    }
}

fn detect_kde(path: &str) -> Mode {
    match Ini::load_from_file(path) {
        Ok(cfg) => {
            let section = match cfg.section(Some("Colors:Window")) {
                Some(section) => section,
                None => return Mode::Light,
            };
            let values = match section.get("BackgroundNormal") {
                Some(string) => string,
                None => return Mode::Light,
            };
            let rgb = values
                .split(',')
                .map(|s| s.parse::<u32>().unwrap_or(255))
                .collect::<Vec<u32>>();
            let rgb = if rgb.len() > 2 {
                rgb
            } else {
                vec![255, 255, 255]
            };
            let (r, g, b) = (rgb[0], rgb[1], rgb[2]);
            Mode::rgb(r, g, b)
        }
        Err(_) => Mode::Light,
    }
}

pub fn detect() -> Mode {
    match get_freedesktop_color_scheme() {
        Some(mode) => mode,
        // Other desktop environments are still being worked on, fow now, only the following implementations work.
        None => match DesktopEnvironment::detect() {
            DesktopEnvironment::Kde => {
                let path = if Path::new(XDG_KDEGLOBALS).exists() {
                    PathBuf::from(XDG_KDEGLOBALS)
                } else {
                    dirs::home_dir().unwrap().join(".config/kdeglobals")
                };
                detect_kde(path.to_str().unwrap())
            }
            DesktopEnvironment::Cinnamon => detect_gtk("/org/cinnamon/desktop/interface/gtk-theme"),
            DesktopEnvironment::Gnome => detect_gtk("/org/gnome/desktop/interface/gtk-theme"),
            DesktopEnvironment::Mate => detect_gtk("/org/mate/desktop/interface/gtk-theme"),
            DesktopEnvironment::Unity => detect_gtk("/org/gnome/desktop/interface/gtk-theme"),
            _ => Mode::Default,
        },
    }
}
