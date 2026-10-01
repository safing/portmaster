use log::{debug, error};
use serde::Deserialize;
use std::path::Path;
use std::time::Duration;
use tauri::State;

use crate::portmaster::PORTMASTER_BASE_URL;

/// Response of the core's `core/paths` endpoint.
#[derive(Deserialize)]
struct Paths {
    bin: String,
    data: String,
    logs: String,
}

/// The Portmaster directories that the UI may open.
///
/// The UI names a directory by kind only, never by path, so it cannot
/// open arbitrary locations.
#[derive(Deserialize, Clone, Copy, Debug)]
#[serde(rename_all = "lowercase")]
pub enum DirKind {
    Bin,
    Data,
    Logs,
}

/// Opens one of the Portmaster directories in the file manager.
///
/// The directories are requested from the core, as only the core knows
/// them reliably (they may be configured via command line flags).
#[tauri::command]
pub async fn open_dir(client: State<'_, reqwest::Client>, kind: DirKind) -> Result<(), String> {
    let paths = client
        .get(format!("{}core/paths", PORTMASTER_BASE_URL))
        .timeout(Duration::from_secs(10))
        .send()
        .await
        .and_then(|resp| resp.error_for_status())
        .map_err(|err| format!("failed to query paths from core: {}", err))?
        .json::<Paths>()
        .await
        .map_err(|err| format!("failed to parse paths from core: {}", err))?;

    let path = match kind {
        DirKind::Bin => paths.bin,
        DirKind::Data => paths.data,
        DirKind::Logs => paths.logs,
    };
    if path.is_empty() {
        return Err(format!("{:?} directory is not configured", kind));
    }

    let dir = Path::new(&path);
    if !dir.is_dir() {
        return Err(format!("{:?} directory {} does not exist", kind, path));
    }

    debug!("opening {:?} directory {}", kind, path);
    open_directory(dir).map_err(|err| {
        error!("failed to open {:?} directory {}: {}", kind, path, err);
        err
    })
}

/// Opens a directory with the file manager registered for folders.
#[cfg(target_os = "windows")]
fn open_directory(dir: &Path) -> Result<(), String> {
    use std::os::windows::ffi::OsStrExt;
    use windows::core::PCWSTR;
    use windows::Win32::UI::Shell::ShellExecuteW;
    use windows::Win32::UI::WindowsAndMessaging::SW_SHOWNORMAL;

    // ShellExecuteW with the default verb honors a third-party file manager
    // registered for folders. The `open` crate cannot be used here: the shell
    // plugin enables its `shellexecute-on-windows` feature, and Cargo feature
    // unification applies it to our own `open` dependency as well. With that
    // feature, `open` opens directories via SHOpenFolderAndSelectItems, which
    // always uses Explorer (see issue #1834).
    let path: Vec<u16> = dir
        .as_os_str()
        .encode_wide()
        .chain(std::iter::once(0))
        .collect();

    let result = unsafe {
        ShellExecuteW(
            None,
            PCWSTR::null(),
            PCWSTR(path.as_ptr()),
            PCWSTR::null(),
            PCWSTR::null(),
            SW_SHOWNORMAL,
        )
    };

    // Per the Win32 documentation, values greater than 32 indicate success.
    if result.0 as usize > 32 {
        Ok(())
    } else {
        Err(format!(
            "ShellExecuteW failed with code {}",
            result.0 as usize
        ))
    }
}

/// Opens a directory with the default file manager (xdg-open and fallbacks).
#[cfg(not(target_os = "windows"))]
fn open_directory(dir: &Path) -> Result<(), String> {
    open::that_detached(dir).map_err(|err| format!("failed to open directory: {}", err))
}
