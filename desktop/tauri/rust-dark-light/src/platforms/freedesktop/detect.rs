use detect_desktop_environment::DesktopEnvironment;

use crate::Mode;

use super::{dconf_detect, gsetting_detect, kde_detect, CINNAMON, GNOME, MATE};

pub fn detect() -> Mode {
    NonFreeDesktop::detect()
}

/// Detects the color scheme on a platform.
trait ColorScheme {
    fn detect() -> Mode;
}

/// Represents the FreeDesktop platform.
struct FreeDesktop;

/// Represents non FreeDesktop platforms.
struct NonFreeDesktop;

/// Detects the color scheme on FreeDesktop platforms. It makes use of the DBus interface.
impl ColorScheme for FreeDesktop {
    fn detect() -> Mode {
        todo!()
    }
}

/// Detects the color scheme on non FreeDesktop platforms, having a custom implementation for each desktop environment.
impl ColorScheme for NonFreeDesktop {
    fn detect() -> Mode {
        match DesktopEnvironment::detect() {
            Some(mode) => match mode {
                DesktopEnvironment::Kde => match kde_detect() {
                    Ok(mode) => mode,
                    Err(_) => Mode::Default,
                },
                DesktopEnvironment::Cinnamon => dconf_detect(CINNAMON),
                DesktopEnvironment::Gnome => gsetting_detect(),
                DesktopEnvironment::Mate => dconf_detect(MATE),
                DesktopEnvironment::Unity => dconf_detect(GNOME),
                _ => Mode::Default,
            },
            None => Mode::Default,
        }
    }
}
