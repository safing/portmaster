//! Management of the `PortmasterKext` Windows driver service.
//!
//! Port of `windowskext/service.go` and
//! `windows_kext/kextinterface/kext.go`. The service is created on demand,
//! started, opened as a device file and deleted again on shutdown. This
//! module is Windows-only; other platforms get an empty module.

#[cfg(windows)]
mod windows_impl {
    #![allow(unsafe_code)]

    use std::time::{Duration, Instant};

    use crate::sys;
    use crate::v2::KextFile;
    use crate::{Error, Result};

    const STOP_TIMEOUT: Duration = Duration::from_secs(30);
    const START_TIMEOUT: Duration = Duration::from_secs(10);

    /// A handle to the Windows service that owns the kernel driver.
    #[derive(Debug)]
    pub struct KextService {
        handle: sys::Handle,
        driver_name: String,
    }

    impl KextService {
        /// Creates (or recreates) the kernel driver service.
        pub fn create(driver_name: &str, driver_path: &str) -> Result<Self> {
            // SAFETY: OpenSCManagerW with null machine/database opens the local
            // service control manager; the returned handle is validated below.
            let manager = unsafe {
                sys::OpenSCManagerW(
                    std::ptr::null(),
                    std::ptr::null(),
                    sys::SC_MANAGER_ALL_ACCESS,
                )
            };
            let manager = sys::check_handle(manager)?;

            // Keep every fallible step inside a closure so the manager handle
            // is closed exactly once on both success and failure.
            let result = (|| {
                let name = sys::wide(driver_name);
                let path = sys::wide(driver_path);

                // Delete a stale service from a previous run, mirroring Go's
                // `createKextService` / `CreateKextService`.
                // SAFETY: `name` is NUL-terminated by `wide`; manager is valid.
                let existing =
                    unsafe { sys::OpenServiceW(manager, name.as_ptr(), sys::SERVICE_ALL_ACCESS) };
                if !sys::is_invalid(existing) {
                    let mut old = Self {
                        handle: existing,
                        driver_name: driver_name.to_owned(),
                    };
                    let _ = old.stop(true);
                    old.delete()?;
                    old.wait_until_deleted(manager)?;
                }

                // SAFETY: all string pointers are NUL-terminated and live for
                // the duration of the call. Null is passed for optional fields.
                let handle = unsafe {
                    sys::CreateServiceW(
                        manager,
                        name.as_ptr(),
                        name.as_ptr(),
                        sys::SERVICE_ALL_ACCESS,
                        sys::SERVICE_KERNEL_DRIVER,
                        sys::SERVICE_DEMAND_START,
                        sys::SERVICE_ERROR_NORMAL,
                        path.as_ptr(),
                        std::ptr::null(),
                        std::ptr::null_mut(),
                        std::ptr::null(),
                        std::ptr::null(),
                        std::ptr::null(),
                    )
                };
                sys::check_handle(handle).map(|handle| Self {
                    handle,
                    driver_name: driver_name.to_owned(),
                })
            })();
            // SAFETY: manager came from a successful OpenSCManagerW above.
            unsafe {
                sys::CloseServiceHandle(manager);
            }
            result
        }

        /// Starts the driver service, optionally waiting for it to run.
        pub fn start(&mut self, wait: bool) -> Result<()> {
            self.require_valid()?;
            // SAFETY: handle is a valid service handle.
            let started = unsafe { sys::StartServiceW(self.handle, 0, std::ptr::null()) };
            if started == 0 {
                let error = sys::last_error();
                // ERROR_SERVICE_ALREADY_RUNNING (1056) is not a failure.
                let already_running = matches!(
                    &error,
                    Error::Io(e) if e.raw_os_error() == Some(sys::ERROR_SERVICE_ALREADY_RUNNING as i32)
                );
                if !already_running {
                    self.cleanup_failed_start();
                    return Err(error);
                }
            }
            if wait {
                self.wait_for_status(sys::SERVICE_RUNNING, START_TIMEOUT)?;
            }
            Ok(())
        }

        /// Stops the driver service, optionally waiting for it to stop.
        pub fn stop(&mut self, wait: bool) -> Result<()> {
            self.require_valid()?;
            let mut status = sys::ServiceStatus::default();
            // SAFETY: handle and status pointer are valid for the call.
            sys::check(unsafe {
                sys::ControlService(self.handle, sys::SERVICE_CONTROL_STOP, &mut status)
            })?;
            if wait {
                self.wait_for_status(sys::SERVICE_STOPPED, STOP_TIMEOUT)?;
            }
            Ok(())
        }

        /// Deletes the service and closes its handle.
        pub fn delete(&mut self) -> Result<()> {
            self.require_valid()?;
            // SAFETY: handle is a valid service handle.
            sys::check(unsafe { sys::DeleteService(self.handle) })?;
            // SAFETY: handle is still owned by this object and not yet closed.
            sys::check(unsafe { sys::CloseServiceHandle(self.handle) })?;
            self.handle = sys::INVALID_HANDLE_VALUE;
            Ok(())
        }

        /// Opens the driver device file with an overlapped read buffer.
        pub fn open_file(&self, read_buffer_size: usize) -> Result<KextFile> {
            self.require_valid()?;
            let device = sys::wide(&format!(r"\\.\{}", self.driver_name));
            // SAFETY: `device` is NUL-terminated and valid for the call.
            let handle = unsafe {
                sys::CreateFileW(
                    device.as_ptr(),
                    sys::GENERIC_READ | sys::GENERIC_WRITE,
                    0,
                    std::ptr::null_mut(),
                    sys::OPEN_EXISTING,
                    sys::FILE_ATTRIBUTE_NORMAL | sys::FILE_FLAG_OVERLAPPED,
                    0,
                )
            };
            let handle = sys::check_handle(handle)?;
            Ok(KextFile::new(handle, read_buffer_size))
        }

        /// Raw service handle (used when handing ownership to the v1 backend).
        #[must_use]
        pub const fn handle(&self) -> sys::Handle {
            self.handle
        }

        fn require_valid(&self) -> Result<()> {
            if sys::is_invalid(self.handle) {
                Err(Error::Command("kext service not initialized".into()))
            } else {
                Ok(())
            }
        }

        fn wait_for_status(&self, needed: u32, limit: Duration) -> Result<()> {
            let start = Instant::now();
            loop {
                let mut status = sys::ServiceStatus {
                    current_state: sys::SERVICE_NO_CHANGE,
                    ..sys::ServiceStatus::default()
                };
                // SAFETY: handle and status pointer are valid for the call.
                sys::check(unsafe { sys::QueryServiceStatus(self.handle, &mut status) })?;
                if status.current_state == needed {
                    return Ok(());
                }
                if start.elapsed() > limit {
                    return Err(Error::Command("service status time limit reached".into()));
                }
                // Microsoft recommends waiting 1/10 of the reported hint.
                std::thread::sleep(Duration::from_millis(u64::from(status.wait_hint) / 10));
            }
        }

        fn wait_until_deleted(&self, manager: sys::Handle) -> Result<()> {
            let name = sys::wide(&self.driver_name);
            let start = Instant::now();
            loop {
                // SAFETY: name is NUL-terminated and manager is a valid handle.
                let handle =
                    unsafe { sys::OpenServiceW(manager, name.as_ptr(), sys::SERVICE_ALL_ACCESS) };
                if sys::is_invalid(handle) {
                    return Ok(());
                }
                // SAFETY: handle was just opened and is not invalid.
                unsafe {
                    sys::CloseServiceHandle(handle);
                }
                if start.elapsed() > STOP_TIMEOUT {
                    return Err(Error::Command("service deletion time limit reached".into()));
                }
                std::thread::sleep(Duration::from_millis(100));
            }
        }

        fn cleanup_failed_start(&mut self) {
            let mut status = sys::ServiceStatus::default();
            // SAFETY: handle is still a valid (opened) service handle.
            unsafe {
                sys::ControlService(self.handle, sys::SERVICE_CONTROL_STOP, &mut status);
                sys::DeleteService(self.handle);
                sys::CloseServiceHandle(self.handle);
            }
            self.handle = sys::INVALID_HANDLE_VALUE;
        }
    }
}

#[cfg(windows)]
pub(crate) use windows_impl::KextService;
