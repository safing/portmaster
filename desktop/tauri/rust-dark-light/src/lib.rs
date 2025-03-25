//! Detect if dark mode or light mode is enabled.
//!
//! # Examples
//!
//! ```
//! let mode = dark_light::detect();
//!
//! match mode {
//!     // Dark mode
//!     dark_light::Mode::Dark => {},
//!     // Light mode
//!     dark_light::Mode::Light => {},
//!     // Unspecified
//!     dark_light::Mode::Default => {},
//! }
//! ```

mod platforms;
use platforms::platform;

mod utils;
#[cfg(any(
    target_os = "linux",
    target_os = "freebsd",
    target_os = "dragonfly",
    target_os = "netbsd",
    target_os = "openbsd"
))]
use utils::rgb::Rgb;

/// Enum representing dark mode, light mode, or unspecified.
#[derive(Copy, Clone, PartialEq, Eq, Debug)]
pub enum Mode {
    /// Dark mode
    Dark,
    /// Light mode
    Light,
    /// Unspecified
    Default,
}

impl Mode {
    #[allow(dead_code)]
    fn from_bool(b: bool) -> Self {
        if b {
            Mode::Dark
        } else {
            Mode::Light
        }
    }

    #[cfg(any(
        target_os = "linux",
        target_os = "freebsd",
        target_os = "dragonfly",
        target_os = "netbsd",
        target_os = "openbsd"
    ))]
    /// Convert an RGB color to [`Mode`]. The color is converted to grayscale, and if the grayscale value is less than 192, [`Mode::Dark`] is returned. Otherwise, [`Mode::Light`] is returned.
    fn from_rgb(rgb: Rgb) -> Self {
        let window_background_gray = (rgb.0 * 11 + rgb.1 * 16 + rgb.2 * 5) / 32;
        if window_background_gray < 192 {
            Self::Dark
        } else {
            Self::Light
        }
    }
}

/// Detect if light mode or dark mode is enabled. If the mode can’t be detected, fall back to [`Mode::Default`].
pub use platform::detect::detect;
/// Notifies the user if the system theme has been changed.
pub use platform::notify::subscribe;
