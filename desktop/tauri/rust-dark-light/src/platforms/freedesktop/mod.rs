use std::{process::Command, str::FromStr};

use anyhow::Context;
use ini::Ini;

use crate::{utils::rgb::Rgb, Mode};

pub mod detect;
pub mod notify;

const MATE: &str = "/org/mate/desktop/interface/gtk-theme";
const GNOME: &str = "/org/gnome/desktop/interface/gtk-theme";
const CINNAMON: &str = "/org/cinnamon/desktop/interface/gtk-theme";

fn dconf_detect(path: &str) -> Mode {
    match dconf_rs::get_string(path) {
        Ok(theme) => {
            println!("dconf output: {}", theme);
            if theme.is_empty() {
                Mode::Default
            } else {
                if theme.to_lowercase().contains("dark") {
                    Mode::Dark
                } else {
                    Mode::Light
                }
            }
        }
        Err(_) => Mode::Default,
    }
}

pub fn gsetting_detect() -> Mode {
    let mode = match Command::new("gsettings")
        .arg("get")
        .arg("org.gnome.desktop.interface")
        .arg("color-scheme")
        .output()
    {
        Ok(output) => {
            if let Ok(scheme) = String::from_utf8(output.stdout) {
                if scheme.contains("prefer-dark") {
                    Mode::Dark
                } else if scheme.contains("prefer-light") {
                    Mode::Dark
                } else {
                    Mode::Default
                }
            } else {
                Mode::Default
            }
        }
        Err(_) => Mode::Default,
    };

    // Fallback to dconf
    if mode == Mode::Default {
        return dconf_detect(GNOME);
    }

    mode
}

fn kde_detect() -> anyhow::Result<Mode> {
    let xdg = xdg::BaseDirectories::new()?;
    let path = xdg
        .find_config_file("kdeglobals")
        .context("Path not found")?;
    let cfg = Ini::load_from_file(path)?;
    let properties = cfg
        .section(Some("Colors:Window"))
        .context("Failed to get section Colors:Window")?;
    let background = properties
        .get("BackgroundNormal")
        .context("Failed to get BackgroundNormal inside Colors:Window")?;
    let rgb = Rgb::from_str(background).unwrap();
    Ok(Mode::from_rgb(rgb))
}

impl From<ashpd::desktop::settings::ColorScheme> for Mode {
    fn from(value: ashpd::desktop::settings::ColorScheme) -> Self {
        match value {
            ashpd::desktop::settings::ColorScheme::NoPreference => Mode::Default,
            ashpd::desktop::settings::ColorScheme::PreferDark => Mode::Dark,
            ashpd::desktop::settings::ColorScheme::PreferLight => Mode::Light,
        }
    }
}
