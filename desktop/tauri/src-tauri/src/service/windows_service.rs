use std::{
    sync::{Arc, Mutex},
    time::Duration,
};

use windows::{
    core::{HSTRING, PCWSTR},
    Win32::{Foundation::HWND, UI::WindowsAndMessaging::SHOW_WINDOW_CMD},
};
use windows_service::{
    service::{Service, ServiceAccess},
    service_manager::{ServiceManager, ServiceManagerAccess},
};

const SERVICE_NAME: &str = "PortmasterCore";

pub struct WindowsServiceManager {
    manager: Option<ServiceManager>,
    service: Option<Service>,
}

lazy_static! {
    pub static ref SERVICE_MANGER: Arc<Mutex<WindowsServiceManager>> =
        Arc::new(Mutex::new(WindowsServiceManager::new()));
}

impl WindowsServiceManager {
    pub fn new() -> Self {
        Self {
            manager: None,
            service: None,
        }
    }

    fn init_manager(&mut self) -> super::Result<()> {
        // Initialize service manager. This connects to the active service database and can query status.
        let manager = match ServiceManager::local_computer(
            None::<&str>,
            ServiceManagerAccess::ENUMERATE_SERVICE, // Only query status is allowed form non privileged application.
        ) {
            Ok(manager) => manager,
            Err(err) => {
                return Err(windows_to_manager_err(err));
            }
        };
        self.manager = Some(manager);
        Ok(())
    }

    fn open_service(&mut self) -> super::Result<bool> {
        if let None = self.manager {
            self.init_manager()?;
        }

        if let Some(manager) = &self.manager {
            let service = match manager.open_service(SERVICE_NAME, ServiceAccess::QUERY_STATUS) {
                Ok(service) => service,
                Err(_) => {
                    return Ok(false); // Service is not installed.
                }
            };
            // Service is installed and the state can be queried.
            self.service = Some(service);
            return Ok(true);
        }

        return Err(super::ServiceManagerError::WindowsError(
            "failed to initialize manager".to_string(),
        ));
    }
}

impl super::ServiceManager for Arc<Mutex<WindowsServiceManager>> {
    fn status(&self) -> super::Result<super::status::StatusResult> {
        if let Ok(mut manager) = self.lock() {
            if let None = manager.service {
                // Try to open service
                if !manager.open_service()? {
                    // Service is not installed.
                    return Ok(super::status::StatusResult::NotFound);
                }
            }

            if let Some(service) = &manager.service {
                match service.query_status() {
                    Ok(status) => match status.current_state {
                        windows_service::service::ServiceState::Stopped
                        | windows_service::service::ServiceState::StopPending
                        | windows_service::service::ServiceState::PausePending
                        | windows_service::service::ServiceState::StartPending
                        | windows_service::service::ServiceState::ContinuePending
                        | windows_service::service::ServiceState::Paused => {
                            // Stopped or in a transition state.
                            return Ok(super::status::StatusResult::Stopped);
                        }
                        windows_service::service::ServiceState::Running => {
                            // Everything expect Running state is considered stopped.
                            return Ok(super::status::StatusResult::Running);
                        }
                    },
                    Err(err) => {
                        return Err(super::ServiceManagerError::WindowsError(err.to_string()));
                    }
                }
            }
        }
        // This should be unreachable.
        Ok(super::status::StatusResult::NotFound)
    }

    fn start(&self) -> super::Result<super::status::StatusResult> {
        if let Ok(mut service_manager) = self.lock() {
            // Check if service is installed.
            if let None = &service_manager.service {
                if let Err(_) = service_manager.open_service() {
                    return Ok(super::status::StatusResult::NotFound);
                }
            }

            // Run service manager with elevated privileges. This will show access popup.
            unsafe {
                windows::Win32::UI::Shell::ShellExecuteW(
                    HWND::default(),
                    &HSTRING::from("runas"),
                    &HSTRING::from("C:\\Windows\\System32\\sc.exe"),
                    &HSTRING::from(format!("start {}", SERVICE_NAME)),
                    PCWSTR::null(),
                    SHOW_WINDOW_CMD(0),
                );
            }

            // Wait for service to start. Timeout 10s (100 * 100ms).
            if let Some(service) = &service_manager.service {
                for _ in 0..100 {
                    match service.query_status() {
                        Ok(status) => {
                            if let windows_service::service::ServiceState::Running =
                                status.current_state
                            {
                                return Ok(super::status::StatusResult::Running);
                            } else {
                                std::thread::sleep(Duration::from_millis(100));
                            }
                        }
                        Err(err) => return Err(windows_to_manager_err(err)),
                    }
                }
            }
            // Timeout starting the service.
            return Ok(super::status::StatusResult::Stopped);
        }
        return Err(super::ServiceManagerError::WindowsError(
            "failed to start service".to_string(),
        ));
    }
}

fn windows_to_manager_err(err: windows_service::Error) -> super::ServiceManagerError {
    if let windows_service::Error::Winapi(_) = err {
        // Winapi does not contain the full error. Get the actual error from windows.
        return super::ServiceManagerError::WindowsError(
            windows::core::Error::from_win32().to_string(), // Internally will call `GetLastError()` and parse the result.
        );
    } else {
        return super::ServiceManagerError::WindowsError(err.to_string());
    }
}
