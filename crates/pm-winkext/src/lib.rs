//! Windows kernel-extension (kext) interception backend for the Portmaster core.
//!
//! This crate ports the user-space side of the two Go kext integrations:
//!
//! * `service/firewall/interception/windowskext` — the original ioctl-based
//!   driver interface (v1, protocol major version < 2), and
//! * `service/firewall/interception/windowskext2` together with
//!   `windows_kext/kextinterface` — the command/info byte-stream interface
//!   (v2, protocol major version >= 2).
//!
//! The driver itself (`windows_kext/driver`) is deliberately **not** built
//! here. The crate opens and controls an already-installed `PortmasterKext`
//! service/device.
//!
//! # Platform support
//!
//! On Windows, [`Kext`] talks to the driver through tightly scoped FFI calls
//! confined to `sys`, `service`, `v1` and `v2` (each marked
//! `#[allow(unsafe_code)]`). On every other platform the same API exists as a
//! no-op that returns [`Error::UnsupportedPlatform`], so the crate (and crates
//! depending on it) still build and test on Linux. All wire-format types and
//! parsers in [`protocol`] are platform independent and unit-tested on Linux.

pub mod protocol;
mod service;
mod sys;
mod v1;
mod v2;

use std::path::PathBuf;

pub use protocol::{
    BandwidthStatsArray, BandwidthValueV4, BandwidthValueV6, Command, ConnectionEndV4,
    ConnectionEndV6, ConnectionStat, ConnectionV4, ConnectionV6, Info, InfoType, KextVerdict,
    LogLine, NetworkVerdict, Version, VerdictInfo, VerdictRequest, VerdictUpdateInfo,
    DRIVER_DEVICE_PATH, DRIVER_NAME, IOCTL_CLEAR_CACHE, IOCTL_GET_CONNECTIONS_STAT,
    IOCTL_GET_PAYLOAD, IOCTL_RECV_VERDICT_REQ, IOCTL_SET_VERDICT, IOCTL_SHUTDOWN_REQUEST,
    IOCTL_UPDATE_VERDICT, IOCTL_VERSION,
};

/// Errors returned by the kext backend.
#[derive(Debug, thiserror::Error)]
pub enum Error {
    /// The driver is not ready to accept commands (Go `ErrKextNotReady`).
    #[error("the windows kernel extension (driver) is not ready to accept commands")]
    KextNotReady,
    /// The packet has no ID, usually because the kext fast-tracked it
    /// (Go `ErrNoPacketID`).
    #[error("the packet has no ID, possibly because it was fast-tracked by the kernel extension")]
    NoPacketId,
    /// The backend was used on a platform without a Windows kernel driver.
    #[error("the windows kernel extension is only available on Windows")]
    UnsupportedPlatform,
    /// An info frame with an unknown type byte was received.
    #[error("unknown kext info type: {0}")]
    UnknownInfoType(u8),
    /// The info frame did not match the announced size.
    #[error("unexpected kext info size")]
    UnexpectedInfoSize,
    /// Reading an info frame failed unexpectedly.
    #[error("unexpected kext read error: {0}")]
    UnexpectedRead(String),
    /// A Windows API or I/O error.
    #[error("kext I/O error: {0}")]
    Io(#[from] std::io::Error),
    /// A driver command failed with the given message.
    #[error("kext command failed: {0}")]
    Command(String),
}

/// Convenience result alias for this crate.
pub type Result<T> = std::result::Result<T, Error>;

/// Which protocol version the connected driver speaks, selected from its
/// reported major version (see Go `interception_windows.go`).
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Backend {
    /// ioctl-based v1 interface (`windowskext`).
    V1,
    /// command/info stream v2 interface (`windowskext2`).
    V2,
}

/// Handle to the Portmaster kernel extension.
///
/// Create it with [`Kext::new`], configure the driver location with
/// [`Kext::init`] and bring the driver up with [`Kext::start`]. The heavy
/// lifting lives in the platform-specific submodules; this type only keeps
/// the shared state and dispatches to v1 or v2 based on the driver version.
#[derive(Debug)]
pub struct Kext {
    driver_path: PathBuf,
    version: Option<Version>,
    #[cfg(windows)]
    service: Option<service::KextService>,
    #[cfg(windows)]
    file: Option<v2::KextFile>,
}

impl Default for Kext {
    fn default() -> Self {
        Self::new()
    }
}

impl Kext {
    /// Creates an uninitialized kext handle.
    #[must_use]
    pub fn new() -> Self {
        Self {
            driver_path: PathBuf::from(DRIVER_NAME),
            version: None,
            #[cfg(windows)]
            service: None,
            #[cfg(windows)]
            file: None,
        }
    }

    /// Remembers the path of the driver `.sys` file used by [`Kext::start`].
    pub fn init(&mut self, path: impl Into<PathBuf>) {
        self.driver_path = path.into();
    }

    /// Returns the path configured with [`Kext::init`].
    #[must_use]
    pub fn driver_path(&self) -> &std::path::Path {
        &self.driver_path
    }

    /// Returns the version reported by the driver, if it has been started.
    #[must_use]
    pub const fn version(&self) -> Option<Version> {
        self.version
    }

    /// Returns the backend selected after [`Kext::start`].
    #[must_use]
    pub fn backend(&self) -> Option<Backend> {
        self.version.map(|version| {
            if version.major < 2 {
                Backend::V1
            } else {
                Backend::V2
            }
        })
    }

    /// Installs/starts the driver service, opens the device and reads its
    /// version. The driver binary is expected to already exist on disk; this
    /// crate never builds or ships it.
    pub fn start(&mut self) -> Result<Version> {
        #[cfg(windows)]
        {
            let path = self.driver_path.to_string_lossy().into_owned();
            let mut svc = service::KextService::create(DRIVER_NAME, &path)?;
            svc.start(true)?;
            let file = svc.open_file(v2::DEFAULT_READ_BUFFER)?;
            let version = v2::read_version(&file)?;

            self.service = Some(svc);
            self.file = Some(file);
            self.version = Some(version);
            Ok(version)
        }
        #[cfg(not(windows))]
        {
            Err(Error::UnsupportedPlatform)
        }
    }

    /// Shuts the driver down, closes the device and removes the service.
    pub fn stop(&mut self) -> Result<()> {
        #[cfg(windows)]
        {
            if let Some(file) = self.file.as_ref() {
                // Best-effort, matching Go's `shutdownRequest`.
                let _ = self.shutdown();
                let _ = file.close();
            }
            self.file = None;
            if let Some(service) = self.service.as_mut() {
                let _ = service.stop(true);
                service.delete()?;
            }
            self.service = None;
            self.version = None;
            Ok(())
        }
        #[cfg(not(windows))]
        {
            Err(Error::UnsupportedPlatform)
        }
    }

    /// Sends a shutdown request to the driver.
    pub fn shutdown(&self) -> Result<()> {
        #[cfg(windows)]
        {
            match self.backend() {
                Some(Backend::V1) => v1::shutdown(self.handle()?),
                Some(Backend::V2) => {
                    v2::send_simple(self.file()?, Command::Shutdown)?;
                    Ok(())
                }
                None => Err(Error::KextNotReady),
            }
        }
        #[cfg(not(windows))]
        {
            Err(Error::UnsupportedPlatform)
        }
    }

    /// Clears the driver's connection verdict cache.
    pub fn clear_cache(&self) -> Result<()> {
        #[cfg(windows)]
        {
            match self.backend() {
                Some(Backend::V1) => v1::clear_cache(self.handle()?),
                Some(Backend::V2) => v2::send_simple(self.file()?, Command::ClearCache),
                None => Err(Error::KextNotReady),
            }
        }
        #[cfg(not(windows))]
        {
            Err(Error::UnsupportedPlatform)
        }
    }

    /// Requests the driver's log output (v2 only).
    pub fn send_log_request(&self) -> Result<()> {
        #[cfg(windows)]
        {
            self.require_v2()?;
            v2::send_simple(self.file()?, Command::GetLogs)
        }
        #[cfg(not(windows))]
        {
            Err(Error::UnsupportedPlatform)
        }
    }

    /// Requests the driver's bandwidth statistics (v2 only).
    pub fn send_bandwidth_stats_request(&self) -> Result<()> {
        #[cfg(windows)]
        {
            self.require_v2()?;
            v2::send_simple(self.file()?, Command::BandwidthStats)
        }
        #[cfg(not(windows))]
        {
            Err(Error::UnsupportedPlatform)
        }
    }

    /// Requests the driver to clean up ended connections (v2 only).
    pub fn send_clean_ended_connections(&self) -> Result<()> {
        #[cfg(windows)]
        {
            self.require_v2()?;
            v2::send_simple(self.file()?, Command::CleanEndedConnections)
        }
        #[cfg(not(windows))]
        {
            Err(Error::UnsupportedPlatform)
        }
    }

    /// Applies a verdict with the given ID.
    pub fn set_verdict(&self, id: u64, verdict: KextVerdict) -> Result<()> {
        #[cfg(windows)]
        {
            match self.backend() {
                Some(Backend::V1) => {
                    let id = u32::try_from(id).map_err(|_| Error::NoPacketId)?;
                    v1::set_verdict(self.handle()?, id, verdict.as_u8() as i8)
                }
                Some(Backend::V2) => v2::send_verdict(self.file()?, id, verdict),
                None => Err(Error::KextNotReady),
            }
        }
        #[cfg(not(windows))]
        {
            let _ = (id, verdict);
            Err(Error::UnsupportedPlatform)
        }
    }

    /// Updates the verdict of an already-cached IPv4 connection.
    pub fn update_verdict_v4(
        &self,
        protocol: u8,
        local_ip: [u8; 4],
        local_port: u16,
        remote_ip: [u8; 4],
        remote_port: u16,
        verdict: KextVerdict,
    ) -> Result<()> {
        #[cfg(windows)]
        {
            match self.backend() {
                Some(Backend::V1) => v1::update_verdict(
                    self.handle()?,
                    protocol,
                    &local_ip,
                    local_port,
                    &remote_ip,
                    remote_port,
                    verdict.as_u8() as i8,
                ),
                Some(Backend::V2) => v2::send_update_v4(
                    self.file()?,
                    protocol,
                    local_ip,
                    local_port,
                    remote_ip,
                    remote_port,
                    verdict,
                ),
                None => Err(Error::KextNotReady),
            }
        }
        #[cfg(not(windows))]
        {
            let _ = (
                protocol,
                local_ip,
                local_port,
                remote_ip,
                remote_port,
                verdict,
            );
            Err(Error::UnsupportedPlatform)
        }
    }

    /// Updates the verdict of an already-cached IPv6 connection.
    pub fn update_verdict_v6(
        &self,
        protocol: u8,
        local_ip: [u8; 16],
        local_port: u16,
        remote_ip: [u8; 16],
        remote_port: u16,
        verdict: KextVerdict,
    ) -> Result<()> {
        #[cfg(windows)]
        {
            match self.backend() {
                Some(Backend::V1) => v1::update_verdict(
                    self.handle()?,
                    protocol,
                    &local_ip,
                    local_port,
                    &remote_ip,
                    remote_port,
                    verdict.as_u8() as i8,
                ),
                Some(Backend::V2) => v2::send_update_v6(
                    self.file()?,
                    protocol,
                    local_ip,
                    local_port,
                    remote_ip,
                    remote_port,
                    verdict,
                ),
                None => Err(Error::KextNotReady),
            }
        }
        #[cfg(not(windows))]
        {
            let _ = (
                protocol,
                local_ip,
                local_port,
                remote_ip,
                remote_port,
                verdict,
            );
            Err(Error::UnsupportedPlatform)
        }
    }

    /// Receives the next v2 info frame. `Ok(None)` means the driver closed the
    /// connection.
    pub fn recv_info(&mut self) -> Result<Option<Info>> {
        #[cfg(windows)]
        {
            self.require_v2()?;
            let file = self.file.as_mut().ok_or(Error::KextNotReady)?;
            v2::recv_info(file)
        }
        #[cfg(not(windows))]
        {
            Err(Error::UnsupportedPlatform)
        }
    }

    /// Receives the next v1 verdict request. `Ok(None)` means no request is
    /// available.
    pub fn recv_verdict_request(&self) -> Result<Option<VerdictRequest>> {
        #[cfg(windows)]
        {
            self.require_v1()?;
            v1::recv_verdict_request(self.handle()?)
        }
        #[cfg(not(windows))]
        {
            Err(Error::UnsupportedPlatform)
        }
    }

    /// Loads the raw payload for a v1 packet ID.
    pub fn get_payload(&self, id: u32, size: u32) -> Result<Vec<u8>> {
        #[cfg(windows)]
        {
            self.require_v1()?;
            v1::get_payload(self.handle()?, id, size)
        }
        #[cfg(not(windows))]
        {
            let _ = (id, size);
            Err(Error::UnsupportedPlatform)
        }
    }

    /// Reads the connection statistics table (v1 only).
    pub fn connection_stats(&self) -> Result<Vec<ConnectionStat>> {
        #[cfg(windows)]
        {
            self.require_v1()?;
            v1::connection_stats(self.handle()?)
        }
        #[cfg(not(windows))]
        {
            Err(Error::UnsupportedPlatform)
        }
    }

    // ---- Windows-only helpers ---------------------------------------------

    /// Returns `(kext device handle, service handle)` as raw pointer values.
    ///
    /// Mirrors Go's `GetKextHandle`/`GetKextServiceHandle`, used when an old
    /// (v1) driver is managed through the v2 service wrapper.
    #[cfg(windows)]
    #[must_use]
    pub fn raw_handles(&self) -> Option<(usize, usize)> {
        let file = self.file.as_ref()?;
        let service = self.service.as_ref()?;
        Some((file.handle() as usize, service.handle() as usize))
    }

    #[cfg(windows)]
    fn require_v1(&self) -> Result<()> {
        match self.backend() {
            Some(Backend::V1) => Ok(()),
            _ => Err(Error::Command("v1 kext command on v2 backend".into())),
        }
    }

    #[cfg(windows)]
    fn require_v2(&self) -> Result<()> {
        match self.backend() {
            Some(Backend::V2) => Ok(()),
            _ => Err(Error::Command("v2 kext command on v1 backend".into())),
        }
    }

    #[cfg(windows)]
    fn file(&self) -> Result<&v2::KextFile> {
        self.file.as_ref().ok_or(Error::KextNotReady)
    }

    #[cfg(windows)]
    fn handle(&self) -> Result<sys::Handle> {
        self.file
            .as_ref()
            .map(v2::KextFile::handle)
            .ok_or(Error::KextNotReady)
    }
}

/// Returns the human-readable name of a severity byte emitted by the kext.
#[must_use]
pub const fn severity_name(severity: u8) -> &'static str {
    match severity {
        1 => "trace",
        2 => "debug",
        3 => "info",
        4 => "warning",
        5 => "error",
        6 => "critical",
        7 => "disabled",
        _ => "unknown",
    }
}
