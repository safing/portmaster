use log::{debug, error};
use tauri::{
    image::Image, AppHandle, Listener, Manager, Result, Theme, UserAttentionType, WebviewUrl,
    WebviewWindow, WebviewWindowBuilder,
};

use crate::{portmaster::PortmasterExt, traymenu};

const LIGHT_PM_ICON: &'static [u8] =
    include_bytes!("../../../../assets/data/icons/pm_light_512.png");
const DARK_PM_ICON: &'static [u8] = include_bytes!("../../../../assets/data/icons/pm_dark_512.png");

/// Either returns the existing "main" window or creates a new one.
///
/// The window is not automatically shown (i.e it starts hidden).
/// If a new main window is created (i.e. the tauri app was minimized to system-tray)
/// then the window will be automatically navigated to the Portmaster UI endpoint
/// if ::websocket::is_portapi_reachable returns true.
///
/// Either the existing or the newly created window is returned.
pub fn create_main_window(app: &AppHandle) -> Result<WebviewWindow> {
    let mut window = if let Some(window) = app.get_webview_window("main") {
        debug!("[tauri] main window already created");
        window
    } else {
        debug!("[tauri] creating main window");

        let res = WebviewWindowBuilder::new(app, "main", WebviewUrl::App("index.html".into()))
            .title("Portmaster")
            .visible(false)
            .inner_size(1200.0, 700.0)
            .min_inner_size(800.0, 600.0)
            .theme(Some(Theme::Dark))
            .build();

        match res {
            Ok(win) => {
                win.once("tauri://error", |event| {
                    error!("failed to open tauri window: {}", event.payload());
                });

                win
            }
            Err(err) => {
                error!("[tauri] failed to create main window: {}", err.to_string());

                return Err(err);
            }
        }
    };

    // If the window is not yet navigated to the Portmaster UI, do it now.
    may_navigate_to_ui(&mut window, false);
    set_window_icon(&window);

    #[cfg(debug_assertions)]
    if let Ok(_) = std::env::var("TAURI_SHOW_IMMEDIATELY") {
        debug!("[tauri] TAURI_SHOW_IMMEDIATELY is set, opening window");

        if let Err(err) = window.show() {
            error!("[tauri] failed to show window: {}", err.to_string());
        }
    }

    Ok(window)
}

pub fn create_splash_window(app: &AppHandle) -> Result<WebviewWindow> {
    if let Some(window) = app.get_webview_window("splash") {
        let _ = window.show();
        Ok(window)
    } else {
        let window = WebviewWindowBuilder::new(app, "splash", WebviewUrl::App("index.html".into()))
            .center()
            .closable(false)
            .focused(true)
            .resizable(false)
            .visible(true)
            .title("Portmaster")
            .inner_size(600.0, 250.0)
            .build()?;
        set_window_icon(&window);

        let _ = window.request_user_attention(Some(UserAttentionType::Informational));

        Ok(window)
    }
}

pub fn close_splash_window(app: &AppHandle) -> Result<()> {
    if let Some(window) = app.get_webview_window("splash") {
        let _ = window.hide();
        return window.destroy();
    }
    return Err(tauri::Error::WindowNotFound);
}

pub fn hide_splash_window(app: &AppHandle) -> Result<()> {
    if let Some(window) = app.get_webview_window("splash") {
        return window.hide();
    }
    return Err(tauri::Error::WindowNotFound);
}

pub fn set_window_icon(window: &WebviewWindow) {
    let mut mode = if let Ok(value) = traymenu::USER_THEME.read() {
        *value
    } else {
        dark_light::Mode::Default
    };

    if mode == dark_light::Mode::Default {
        mode = dark_light::detect();
    }

    let _ = match mode {
        dark_light::Mode::Light => window.set_icon(Image::from_bytes(DARK_PM_ICON).unwrap()),
        _ => window.set_icon(Image::from_bytes(LIGHT_PM_ICON).unwrap()),
    };
}

/// Opens a window for the tauri application.
///
/// If the main window has already been created, it is instructed to
/// show even if we're currently not connected to Portmaster.
/// This is safe since the main-window will only be created if Portmaster API
/// was reachable so the angular application must have finished bootstrapping.
///
/// If there's not main window and the Portmaster API is reachable we create a new
/// main window.
///
/// If the Portmaster API is unreachable and there's no main window yet, we show the
/// splash-screen window.
pub fn open_window(app: &AppHandle) -> Result<WebviewWindow> {
    if app.portmaster().is_reachable() {
        match app.get_webview_window("main") {
            Some(win) => {
                app.portmaster().show_window();
                let _ = win.show();
                let _ = win.set_focus();
                set_window_icon(&win);
                Ok(win)
            }
            None => {
                app.portmaster().show_window();

                create_main_window(app)
            }
        }
    } else {
        debug!("Show splash screen");
        create_splash_window(app)
    }
}

/// If the Portmaster Websocket database API is reachable the window will be navigated
/// to the HTTP endpoint of Portmaster to load the UI from there.
///
/// Note that only happens if the window URL does not already point to the PM API.
///
/// In #[cfg(debug_assertions)] the TAURI_PM_URL environment variable will be used
/// if set.
/// Otherwise or in release builds, it will be navigated to http://127.0.0.1:817.
pub fn may_navigate_to_ui(win: &mut WebviewWindow, force: bool) {
    if !win.app_handle().portmaster().is_reachable() && !force {
        error!("[tauri] portmaster API is not reachable, not navigating");

        return;
    }
    if force || win.label().eq("main") {
        #[cfg(debug_assertions)]
        if let Ok(target_url) = std::env::var("TAURI_PM_URL") {
            debug!("[tauri] navigating to {}", target_url);

            _ = win.navigate(target_url.parse().unwrap());

            return;
        }

        #[cfg(debug_assertions)]
        {
            // Only for dev build
            // Allow connection to http://localhost:4200
            let capabilities = include_str!("../capabilities/default.json")
                .replace("http://localhost:817", "http://localhost:4200");
            let _ = win.add_capability(capabilities);
            debug!("[tauri] navigating to http://localhost:4200");
            _ = win.navigate("http://localhost:4200".parse().unwrap());
        }

        #[cfg(not(debug_assertions))]
        {
            _ = win.navigate("http://localhost:817".parse().unwrap());
        }
    } else {
        error!(
            "not navigating to user interface: current url: {}",
            win.url().unwrap().as_str()
        );
    }
}
