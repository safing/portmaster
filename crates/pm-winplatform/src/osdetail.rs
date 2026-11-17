//! Windows OS details: services, shells, DNS cache, versions and colors.
//!
//! Port of Go `base/utils/osdetail`. The string parsers (`svchost` task lists,
//! `sc query` output and version comparison) are pure and tested on Linux; the
//! commands and the console FFI only exist under `#[cfg(windows)]`.

use std::cmp::Ordering;
use std::collections::HashMap;

/// Errors of the OS detail helpers.
#[derive(Debug, thiserror::Error)]
pub enum OsError {
    /// The operation is not supported on the current platform.
    #[error("not supported")]
    NotSupported,
    /// The requested data was not found.
    #[error("not found")]
    NotFound,
    /// A command succeeded but printed nothing.
    #[error("command succeeded with empty output")]
    EmptyOutput,
    /// The service cannot be stopped.
    #[error("the service is not stoppable")]
    ServiceNotStoppable,
    /// No service with the given PID exists.
    #[error("no service with the given PID was found")]
    ServiceNotFound,
    /// A command failed.
    #[error("command failed: {0}")]
    Command(String),
    /// `sc query` returned an unrecognised status.
    #[error("unknown service status")]
    UnknownServiceStatus,
    /// A Windows version string could not be parsed.
    #[error("invalid Windows version {0}")]
    InvalidVersion(String),
}

/// Result type of the OS detail helpers.
pub type Result<T> = std::result::Result<T, OsError>;

/// Service status codes from Go `service_windows.go`.
#[repr(u8)]
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum ServiceStatus {
    /// The status could not be determined.
    Unknown = 0,
    /// Running and stoppable.
    RunningStoppable = 1,
    /// Running but not stoppable.
    RunningNotStoppable = 2,
    /// Starting up.
    StartPending = 3,
    /// Shutting down.
    StopPending = 4,
    /// Not running.
    Stopped = 5,
}

// ---------------------------------------------------------------------------
// Pure parsers
// ---------------------------------------------------------------------------

/// Parses the output of `sc query <name>`, like Go `GetServiceStatus`.
///
/// The order of the checks matches Go exactly, including the quirk that
/// `START_PENDING` maps to [`ServiceStatus::StopPending`].
pub fn parse_service_status(output: &str) -> Result<ServiceStatus> {
    if output.contains("RUNNING") {
        if output.contains("NOT_STOPPABLE") {
            return Ok(ServiceStatus::RunningNotStoppable);
        }
        return Ok(ServiceStatus::RunningStoppable);
    }
    if output.contains("STOP_PENDING") {
        return Ok(ServiceStatus::StopPending);
    }
    if output.contains("STOPPED") {
        return Ok(ServiceStatus::Stopped);
    }
    if output.contains("START_PENDING") {
        return Ok(ServiceStatus::StopPending);
    }
    Err(OsError::UnknownServiceStatus)
}

/// Parses the output of `tasklist /svc /fi "imagename eq svchost.exe"`.
///
/// Returns a map of PID to the service names hosted by that `svchost.exe`.
/// This is the pure half of Go's `GetAllServiceNames`.
pub fn parse_svchost_tasklist(output: &str) -> HashMap<i32, Vec<String>> {
    let mut lines = output.lines();

    // Skip the header, which ends with a line starting with '='.
    for line in lines.by_ref() {
        if line.starts_with('=') {
            break;
        }
    }

    let mut collection: HashMap<i32, Vec<String>> = HashMap::new();
    let mut pid: i32 = 0;
    let mut services: Vec<String> = Vec::new();

    for line in lines {
        let fields: Vec<&str> = line.split_whitespace().collect();
        if fields.is_empty() {
            continue;
        }

        let mut rest: &[&str] = &fields;
        if fields[0] == "svchost.exe" {
            if pid != 0 {
                collection.insert(pid, std::mem::take(&mut services));
            }
            pid = 0;
            services = Vec::with_capacity(fields.len());
            if fields.len() < 3 {
                continue;
            }
            if let Ok(parsed) = fields[1].parse::<i32>() {
                pid = parsed;
            }
            rest = &fields[2..];
        }

        for field in rest {
            services.push(field.trim().trim_matches(',').to_string());
        }
    }

    if pid != 0 {
        collection.insert(pid, services);
    }

    collection
}

/// A numeric Windows version, comparable component by component like Go's
/// `hashicorp/go-version`.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct WindowsVersion {
    /// Version components, e.g. `[10, 0, 19041]`.
    pub parts: Vec<u64>,
}

impl WindowsVersion {
    /// Parses a dotted version string.
    pub fn parse(input: &str) -> Result<Self> {
        let parts = input
            .split('.')
            .map(|part| {
                part.trim()
                    .parse::<u64>()
                    .map_err(|_| OsError::InvalidVersion(input.to_string()))
            })
            .collect::<Result<Vec<_>>>()?;
        if parts.is_empty() {
            return Err(OsError::InvalidVersion(input.to_string()));
        }
        Ok(Self { parts })
    }

    /// Compares two versions, treating missing trailing components as zero.
    pub fn cmp_versions(&self, other: &Self) -> Ordering {
        let len = self.parts.len().max(other.parts.len());
        for index in 0..len {
            let left = self.parts.get(index).copied().unwrap_or(0);
            let right = other.parts.get(index).copied().unwrap_or(0);
            match left.cmp(&right) {
                Ordering::Equal => {}
                ordering => return ordering,
            }
        }
        Ordering::Equal
    }

    /// Whether `self` is at least `other`.
    pub fn at_least(&self, other: &Self) -> bool {
        self.cmp_versions(other) != Ordering::Less
    }
}

/// Maps a marketing Windows version to its NT version, like Go
/// `IsAtLeastWindowsVersion`.
pub fn nt_version_for(windows_version: &str) -> Result<&'static str> {
    match windows_version {
        "7" => Ok("6.1"),
        "8" => Ok("6.2"),
        "8.1" => Ok("6.3"),
        "10" => Ok("10"),
        other => Err(OsError::InvalidVersion(other.to_string())),
    }
}

/// Pure version check used by both the FFI path and the Linux tests.
pub fn is_at_least(current: &str, required: &str) -> Result<bool> {
    let current = WindowsVersion::parse(current)?;
    let required = WindowsVersion::parse(required)?;
    Ok(current.at_least(&required))
}

/// Strips the noise emitted by `cmd.exe /c chcp 65001`, like Go's
/// `RunTerminalCmd` post-processing.
pub fn clean_terminal_output(output: &[u8], separator: &str) -> Result<Vec<u8>> {
    let needle = separator.as_bytes();
    let index = output
        .windows(needle.len())
        .position(|window| window == needle)
        .ok_or_else(|| OsError::Command("failed to post-process output: separator not found".into()))?;
    // Skip the separator and the trailing CRLF.
    let start = index + needle.len() + 2;
    if start > output.len() {
        return Ok(Vec::new());
    }
    Ok(output[start..].to_vec())
}

// ---------------------------------------------------------------------------
// Windows-only console FFI
// ---------------------------------------------------------------------------

#[cfg(windows)]
mod ffi {
    use std::ffi::c_void;

    /// Mirrors `OSVERSIONINFOW` from `winnt.h`.
    #[repr(C)]
    pub(crate) struct OsVersionInfoW {
        pub(crate) dw_os_version_info_size: u32,
        pub(crate) dw_major_version: u32,
        pub(crate) dw_minor_version: u32,
        pub(crate) dw_build_number: u32,
        pub(crate) dw_platform_id: u32,
        pub(crate) sz_csd_version_rlc: [u16; 128],
    }

    impl OsVersionInfoW {
        pub(crate) fn new() -> Self {
            Self {
                dw_os_version_info_size: std::mem::size_of::<Self>() as u32,
                dw_major_version: 0,
                dw_minor_version: 0,
                dw_build_number: 0,
                dw_platform_id: 0,
                sz_csd_version_rlc: [0; 128],
            }
        }
    }

    // The only `unsafe` OS-detail FFI: console mode and NT version queries.
    #[allow(unsafe_code)]
    #[link(name = "kernel32")]
    unsafe extern "system" {
        pub(crate) fn GetConsoleMode(handle: *mut c_void, mode: *mut u32) -> i32;
        pub(crate) fn SetConsoleMode(handle: *mut c_void, mode: u32) -> i32;
        pub(crate) fn GetVersionExW(info: *mut OsVersionInfoW) -> i32;
    }
}

#[cfg(windows)]
const ENABLE_VIRTUAL_TERMINAL_PROCESSING: u32 = 0x0004;

/// Returns the current Windows NT version string (e.g. `10.0.19041`).
#[cfg(windows)]
#[allow(unsafe_code)]
pub fn windows_nt_version() -> Result<String> {
    let mut info = ffi::OsVersionInfoW::new();
    let ok = unsafe { ffi::GetVersionExW(&mut info) };
    if ok == 0 {
        return Err(OsError::Command("failed to obtain Windows version".into()));
    }
    Ok(format!(
        "{}.{}.{}",
        info.dw_major_version, info.dw_minor_version, info.dw_build_number
    ))
}

/// Non-Windows stub for the version lookup.
#[cfg(not(windows))]
pub fn windows_nt_version() -> Result<String> {
    Err(OsError::NotSupported)
}

/// Whether the current NT version is at least `version`.
#[cfg(windows)]
pub fn is_at_least_nt_version(version: &str) -> Result<bool> {
    is_at_least(&windows_nt_version()?, version)
}

/// Non-Windows stub for the version comparison.
#[cfg(not(windows))]
pub fn is_at_least_nt_version(_version: &str) -> Result<bool> {
    Err(OsError::NotSupported)
}

/// Like [`is_at_least_nt_version`], but returns `default_value` on error.
#[cfg(windows)]
pub fn is_at_least_nt_version_with_default(version: &str, default_value: bool) -> bool {
    is_at_least_nt_version(version).unwrap_or(default_value)
}

/// Non-Windows variant of [`is_at_least_nt_version_with_default`].
#[cfg(not(windows))]
pub fn is_at_least_nt_version_with_default(_version: &str, default_value: bool) -> bool {
    default_value
}

/// Whether the current (marketing) Windows version is at least `version`.
#[cfg(windows)]
pub fn is_at_least_windows_version(version: &str) -> Result<bool> {
    is_at_least_nt_version(nt_version_for(version)?)
}

/// Non-Windows stub for the marketing version comparison.
#[cfg(not(windows))]
pub fn is_at_least_windows_version(_version: &str) -> Result<bool> {
    Err(OsError::NotSupported)
}

/// Enables ANSI escape processing on a Windows console.
///
/// Mirrors Go `EnableColorSupport`: only on Windows 10+, only when stdout is a
/// real console, and cached after the first call.
#[cfg(windows)]
#[allow(unsafe_code)]
pub fn enable_color_support() -> bool {
    use std::io::IsTerminal;
    use std::os::windows::io::AsRawHandle;
    use std::sync::OnceLock;

    static COLOR_SUPPORT: OnceLock<bool> = OnceLock::new();
    *COLOR_SUPPORT.get_or_init(|| {
        if !std::io::stdout().is_terminal() || !is_at_least_nt_version_with_default("10", false) {
            return false;
        }

        let handle = std::io::stdout().as_raw_handle();
        let mut mode = 0u32;
        if unsafe { ffi::GetConsoleMode(handle, &mut mode) } == 0 {
            return false;
        }
        if mode & ENABLE_VIRTUAL_TERMINAL_PROCESSING == 0 {
            mode |= ENABLE_VIRTUAL_TERMINAL_PROCESSING;
            if unsafe { ffi::SetConsoleMode(handle, mode) } == 0 {
                return false;
            }
        }
        true
    })
}

/// Non-Windows stub: colors are always available in the Unix log format.
#[cfg(not(windows))]
pub fn enable_color_support() -> bool {
    true
}

// ---------------------------------------------------------------------------
// Windows-only command helpers
// ---------------------------------------------------------------------------

/// Runs a command and returns its trimmed stdout, like Go `RunCmd`.
#[cfg(windows)]
pub fn run_cmd(command: &[String]) -> Result<Vec<u8>> {
    use std::process::Command;

    let (program, args) = command
        .split_first()
        .ok_or_else(|| OsError::Command("no command supplied".into()))?;
    let output = Command::new(program)
        .args(args)
        .output()
        .map_err(|err| OsError::Command(err.to_string()))?;
    if !output.status.success() {
        return Err(OsError::Command(format!("{program} failed")));
    }
    if !output.stderr.is_empty() {
        let first = output.stderr.split(|byte| *byte == b'\n').next().unwrap_or_default();
        return Err(OsError::Command(
            String::from_utf8_lossy(first).into_owned(),
        ));
    }
    let stdout = output.stdout.trim_ascii();
    if stdout.is_empty() {
        return Err(OsError::EmptyOutput);
    }
    Ok(stdout.to_vec())
}

#[cfg(not(windows))]
pub fn run_cmd(_command: &[String]) -> Result<Vec<u8>> {
    Err(OsError::NotSupported)
}

#[cfg(windows)]
const OUTPUT_SEPARATOR: &str = "pwzzhtuvpwdgozhzbnjj";

/// Runs a PowerShell script with UTF-8 output, like Go `RunPowershellCmd`.
#[cfg(windows)]
pub fn run_powershell_cmd(script: &str) -> Result<Vec<u8>> {
    let script = format!(
        "[System.Console]::OutputEncoding = [System.Text.Encoding]::UTF8\n{script}"
    );
    run_cmd(&[
        "powershell.exe".into(),
        "-ExecutionPolicy".into(),
        "Bypass".into(),
        "-NoProfile".into(),
        "-NonInteractive".into(),
        script,
    ])
}

#[cfg(not(windows))]
pub fn run_powershell_cmd(_script: &str) -> Result<Vec<u8>> {
    Err(OsError::NotSupported)
}

/// Runs a `cmd.exe` command with UTF-8 output, like Go `RunTerminalCmd`.
#[cfg(windows)]
pub fn run_terminal_cmd(command: &[String]) -> Result<Vec<u8>> {
    let mut args = vec![
        "cmd.exe".to_string(),
        "/c".into(),
        "chcp".into(),
        "65001".into(),
        "&".into(),
        "echo".into(),
        OUTPUT_SEPARATOR.into(),
        "&".into(),
    ];
    args.extend_from_slice(command);
    let output = run_cmd(&args)?;
    clean_terminal_output(&output, OUTPUT_SEPARATOR)
}

#[cfg(not(windows))]
pub fn run_terminal_cmd(_command: &[String]) -> Result<Vec<u8>> {
    Err(OsError::NotSupported)
}

#[cfg(windows)]
const DNS_CACHE_KEY: &str = r"HKEY_LOCAL_MACHINE\SYSTEM\CurrentControlSet\services\Dnscache";

/// Enables the Windows DNS Client service (registry `Start = 2`).
#[cfg(windows)]
pub fn enable_dns_cache() -> Result<()> {
    run_cmd(&[
        "reg".into(),
        "add".into(),
        DNS_CACHE_KEY.into(),
        "/v".into(),
        "Start".into(),
        "/t".into(),
        "REG_DWORD".into(),
        "/d".into(),
        "2".into(),
        "/f".into(),
    ])
    .map(|_| ())
}

#[cfg(not(windows))]
pub fn enable_dns_cache() -> Result<()> {
    Err(OsError::NotSupported)
}

/// Disables the Windows DNS Client service (registry `Start = 4`).
#[cfg(windows)]
pub fn disable_dns_cache() -> Result<()> {
    run_cmd(&[
        "reg".into(),
        "add".into(),
        DNS_CACHE_KEY.into(),
        "/v".into(),
        "Start".into(),
        "/t".into(),
        "REG_DWORD".into(),
        "/d".into(),
        "4".into(),
        "/f".into(),
    ])
    .map(|_| ())
}

#[cfg(not(windows))]
pub fn disable_dns_cache() -> Result<()> {
    Err(OsError::NotSupported)
}

/// Queries a service's status with `sc query`.
#[cfg(windows)]
pub fn get_service_status(name: &str) -> Result<ServiceStatus> {
    let output = run_cmd(&["sc".into(), "query".into(), name.into()])?;
    parse_service_status(&String::from_utf8_lossy(&output))
}

#[cfg(not(windows))]
pub fn get_service_status(_name: &str) -> Result<ServiceStatus> {
    Err(OsError::NotSupported)
}

/// Stops a service, polling its status like Go `StopService`.
#[cfg(windows)]
pub fn stop_service(name: &str) -> Result<()> {
    use std::{thread, time::Duration};

    let mut pending_count = 0;
    loop {
        match get_service_status(name)? {
            ServiceStatus::RunningStoppable => {
                run_cmd(&["sc".into(), "stop".into(), name.into()])?;
            }
            ServiceStatus::RunningNotStoppable => return Err(OsError::ServiceNotStoppable),
            ServiceStatus::StartPending | ServiceStatus::StopPending => {
                pending_count += 1;
                if pending_count > 50 {
                    return Err(OsError::Command("service stuck in pending status (5s)".into()));
                }
            }
            ServiceStatus::Stopped => return Ok(()),
            ServiceStatus::Unknown => return Err(OsError::UnknownServiceStatus),
        }
        thread::sleep(Duration::from_millis(100));
    }
}

#[cfg(not(windows))]
pub fn stop_service(_name: &str) -> Result<()> {
    Err(OsError::NotSupported)
}

/// Starts a service, polling its status (Go's `SartService`).
#[cfg(windows)]
pub fn start_service(name: &str) -> Result<()> {
    use std::{thread, time::Duration};

    let mut pending_count = 0;
    loop {
        match get_service_status(name)? {
            ServiceStatus::RunningStoppable | ServiceStatus::RunningNotStoppable => return Ok(()),
            ServiceStatus::StartPending | ServiceStatus::StopPending => {
                pending_count += 1;
                if pending_count > 50 {
                    return Err(OsError::Command("service stuck in pending status (5s)".into()));
                }
            }
            ServiceStatus::Stopped => {
                run_cmd(&["sc".into(), "start".into(), name.into()])?;
            }
            ServiceStatus::Unknown => return Err(OsError::UnknownServiceStatus),
        }
        thread::sleep(Duration::from_millis(100));
    }
}

#[cfg(not(windows))]
pub fn start_service(_name: &str) -> Result<()> {
    Err(OsError::NotSupported)
}

/// Returns all `svchost.exe` service names, cached like Go `GetAllServiceNames`.
#[cfg(windows)]
pub fn get_all_service_names() -> Result<HashMap<i32, Vec<String>>> {
    use std::sync::Mutex;

    static SERVICE_NAMES: Mutex<Option<HashMap<i32, Vec<String>>>> = Mutex::new(None);
    let mut cache = SERVICE_NAMES.lock().expect("lock poisoned");
    if let Some(names) = cache.as_ref() {
        return Ok(names.clone());
    }

    let output = run_cmd(&[
        "tasklist".into(),
        "/svc".into(),
        "/fi".into(),
        "imagename eq svchost.exe".into(),
    ])?;
    let names = parse_svchost_tasklist(&String::from_utf8_lossy(&output));
    *cache = Some(names.clone());
    Ok(names)
}

#[cfg(not(windows))]
pub fn get_all_service_names() -> Result<HashMap<i32, Vec<String>>> {
    Err(OsError::NotSupported)
}

/// Returns the services hosted by the `svchost.exe` with the given PID.
#[cfg(windows)]
pub fn get_service_names(pid: i32) -> Result<Vec<String>> {
    get_all_service_names()?
        .remove(&pid)
        .ok_or(OsError::ServiceNotFound)
}

#[cfg(not(windows))]
pub fn get_service_names(_pid: i32) -> Result<Vec<String>> {
    Err(OsError::NotSupported)
}

#[cfg(test)]
mod tests {
    use super::*;

    const TASKLIST: &str = "\
Image Name                     PID Services
============================= ===== ==========================================

svchost.exe                    824   RpcSs, PlugPlay
svchost.exe                    900   EventLog
svchost.exe                   1104   CDPUserSvc_1bf5729, WpnUserSvc_1bf5729
";

    #[test]
    fn parses_svchost_services() {
        let map = parse_svchost_tasklist(TASKLIST);
        assert_eq!(
            map.get(&824),
            Some(&vec!["RpcSs".to_string(), "PlugPlay".to_string()])
        );
        assert_eq!(map.get(&900), Some(&vec!["EventLog".to_string()]));
        assert_eq!(
            map.get(&1104),
            Some(&vec![
                "CDPUserSvc_1bf5729".to_string(),
                "WpnUserSvc_1bf5729".to_string(),
            ])
        );
    }

    #[test]
    fn parses_service_statuses() {
        assert_eq!(
            parse_service_status("STATE : 4 RUNNING\n").unwrap(),
            ServiceStatus::RunningStoppable
        );
        assert_eq!(
            parse_service_status("STATE : 4 RUNNING\nNOT_STOPPABLE\n").unwrap(),
            ServiceStatus::RunningNotStoppable
        );
        assert_eq!(
            parse_service_status("STATE : 3 STOP_PENDING").unwrap(),
            ServiceStatus::StopPending
        );
        assert_eq!(
            parse_service_status("STATE : 1 STOPPED").unwrap(),
            ServiceStatus::Stopped
        );
        // Go maps START_PENDING to StopPending.
        assert_eq!(
            parse_service_status("STATE : 2 START_PENDING").unwrap(),
            ServiceStatus::StopPending
        );
        assert!(parse_service_status("garbage").is_err());
    }

    #[test]
    fn compares_versions() {
        assert!(is_at_least("10.0.19041", "10").unwrap());
        assert!(is_at_least("6.3.9600", "6.1").unwrap());
        assert!(!is_at_least("6.1.7601", "10").unwrap());
        assert_eq!(nt_version_for("7").unwrap(), "6.1");
        assert_eq!(nt_version_for("8.1").unwrap(), "6.3");
        assert!(nt_version_for("11").is_err());
        assert!(WindowsVersion::parse("x.y").is_err());
    }

    #[test]
    fn cleans_terminal_output() {
        let separator = "pwzzhtuvpwdgozhzbnjj";
        let raw = b"chcp 65001\nMicrosoft Windows [Version 10.0]\npwzzhtuvpwdgozhzbnjj\r\nreal output";
        assert_eq!(
            clean_terminal_output(raw, separator).unwrap(),
            b"real output".to_vec()
        );
        assert!(clean_terminal_output(b"nothing", separator).is_err());
    }
}
