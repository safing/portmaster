use super::PortmasterInterface;
use crate::service::get_service_manager;
use crate::service::ServiceManager;
use log::debug;
use std::sync::atomic::Ordering;
use tauri::{Emitter, Runtime, State, Window};

pub type Result = std::result::Result<String, String>;

#[derive(Clone, serde::Serialize, serde::Deserialize)]
pub struct Error {
    pub error: String,
}

#[tauri::command]
pub fn should_show<R: Runtime>(
    _window: Window<R>,
    portmaster: State<'_, PortmasterInterface<R>>,
) -> Result {
    if portmaster.get_show_after_bootstrap() {
        debug!("[tauri:rpc:should_show] application should show after bootstrap");

        Ok("show".to_string())
    } else {
        debug!("[tauri:rpc:should_show] application should hide after bootstrap");

        Ok("hide".to_string())
    }
}

#[tauri::command]
pub fn should_handle_prompts<R: Runtime>(
    _window: Window<R>,
    portmaster: State<'_, PortmasterInterface<R>>,
) -> Result {
    if portmaster.handle_prompts.load(Ordering::Relaxed) {
        Ok("true".to_string())
    } else {
        Ok("false".to_string())
    }
}

#[tauri::command]
pub fn get_state<R: Runtime>(
    _window: Window<R>,
    portmaster: State<'_, PortmasterInterface<R>>,
    key: String,
) -> Result {
    let value = portmaster.get_state(key);

    if let Some(value) = value {
        Ok(value)
    } else {
        Ok("".to_string())
    }
}

#[tauri::command]
pub fn set_state<R: Runtime>(
    _window: Window<R>,
    portmaster: State<'_, PortmasterInterface<R>>,
    key: String,
    value: String,
) -> Result {
    portmaster.set_state(key, value);

    Ok("".to_string())
}

#[cfg(target_os = "linux")]
#[tauri::command]
pub fn get_app_info<R: Runtime>(
    window: Window<R>,
    response_id: String,
    matching_path: String,
    exec_path: String,
    pid: i64,
    cmdline: String,
) -> Result {
    let mut id = response_id;

    let info = crate::xdg::ProcessInfo {
        cmdline,
        exec_path,
        pid,
        matching_path,
    };

    if id == "" {
        id = uuid::Uuid::new_v4().to_string()
    }
    let cloned = id.clone();

    std::thread::spawn(move || match crate::xdg::get_app_info(info) {
        Ok(info) => window.emit(&id, info),
        Err(err) => window.emit(
            &id,
            Error {
                error: err.to_string(),
            },
        ),
    });

    Ok(cloned)
}

#[cfg(target_os = "windows")]
#[tauri::command]
pub fn get_app_info<R: Runtime>(
    window: Window<R>,
    response_id: String,
    _matching_path: String,
    _exec_path: String,
    _pid: i64,
    _cmdline: String,
) -> Result {
    let mut id = response_id;

    if id == "" {
        id = uuid::Uuid::new_v4().to_string()
    }
    let cloned = id.clone();

    std::thread::spawn(move || {
        let _ = window.emit(
            &id,
            Error {
                error: "Unsupported OS".to_string(),
            },
        );
    });

    Ok(cloned)
}

#[tauri::command]
pub fn get_service_manager_status<R: Runtime>(window: Window<R>, response_id: String) -> Result {
    let mut id = response_id;

    if id == "" {
        id = uuid::Uuid::new_v4().to_string();
    }
    let cloned = id.clone();

    std::thread::spawn(move || {
        let result = match get_service_manager() {
            Ok(sm) => sm.status().map_err(|err| err.to_string()),
            Err(err) => Err(err.to_string()),
        };

        match result {
            Ok(result) => window.emit(&id, &result),
            Err(err) => window.emit(&id, Error { error: err }),
        }
    });

    Ok(cloned)
}

#[tauri::command]
pub fn start_service<R: Runtime>(window: Window<R>, response_id: String) -> Result {
    let mut id = response_id;

    if id == "" {
        id = uuid::Uuid::new_v4().to_string();
    }
    let cloned = id.clone();

    std::thread::spawn(move || {
        let result = match get_service_manager() {
            Ok(sm) => sm.start().map_err(|err| err.to_string()),
            Err(err) => Err(err.to_string()),
        };

        match result {
            Ok(result) => window.emit(&id, &result),
            Err(err) => window.emit(&id, Error { error: err }),
        }
    });

    Ok(cloned)
}
