use std::ops::Deref;
use std::sync::atomic::AtomicBool;
use std::sync::{Mutex, RwLock};
use std::{collections::HashMap, sync::atomic::Ordering};

use log::{debug, error};
use tauri::tray::{MouseButton, MouseButtonState};
use tauri::Manager;
use tauri::{
    image::Image,
    menu::{MenuBuilder, MenuItem, MenuItemBuilder, PredefinedMenuItem, SubmenuBuilder},
    tray::{TrayIcon, TrayIconBuilder},
    Wry,
};
use tauri_plugin_window_state::{AppHandleExt, StateFlags};

use crate::config;
use crate::{
    portapi::{
        client::PortAPI,
        message::ParseError,
        models::{
            config::BooleanValue,
            spn::SPNStatus,
            subsystem::{self, Subsystem},
        },
        types::{Request, Response},
    },
    portmaster::PortmasterExt,
    window::{create_main_window, may_navigate_to_ui, open_window},
};
use tauri_plugin_dialog::{DialogExt, MessageDialogButtons};

pub type AppIcon = TrayIcon<Wry>;

static SPN_STATE: AtomicBool = AtomicBool::new(false);

#[derive(Copy, Clone)]
enum IconColor {
    Red,
    Green,
    Blue,
    Yellow,
}

static CURRENT_ICON_COLOR: RwLock<IconColor> = RwLock::new(IconColor::Red);
pub static USER_THEME: RwLock<dark_light::Mode> = RwLock::new(dark_light::Mode::Default);

lazy_static! {
    static ref SPN_STATUS: Mutex<Option<MenuItem<Wry>>> = Mutex::new(None);
    static ref SPN_BUTTON: Mutex<Option<MenuItem<Wry>>> = Mutex::new(None);
    static ref GLOBAL_STATUS: Mutex<Option<MenuItem<Wry>>> = Mutex::new(None);
}

const PM_TRAY_ICON_ID: &'static str = "pm_icon";

// Icons

fn get_theme_mode() -> dark_light::Mode {
    if let Ok(value) = USER_THEME.read() {
        return *value.deref();
    }
    return dark_light::detect();
}

fn get_green_icon() -> &'static [u8] {
    const LIGHT_GREEN_ICON: &'static [u8] =
        include_bytes!("../../../../assets/data/icons/pm_light_green_64.png");
    const DARK_GREEN_ICON: &'static [u8] =
        include_bytes!("../../../../assets/data/icons/pm_dark_green_64.png");

    match get_theme_mode() {
        dark_light::Mode::Light => DARK_GREEN_ICON,
        _ => LIGHT_GREEN_ICON,
    }
}

fn get_blue_icon() -> &'static [u8] {
    const LIGHT_BLUE_ICON: &'static [u8] =
        include_bytes!("../../../../assets/data/icons/pm_light_blue_64.png");
    const DARK_BLUE_ICON: &'static [u8] =
        include_bytes!("../../../../assets/data/icons/pm_dark_blue_64.png");
    match get_theme_mode() {
        dark_light::Mode::Light => DARK_BLUE_ICON,
        _ => LIGHT_BLUE_ICON,
    }
}

fn get_red_icon() -> &'static [u8] {
    const LIGHT_RED_ICON: &'static [u8] =
        include_bytes!("../../../../assets/data/icons/pm_light_red_64.png");
    const DARK_RED_ICON: &'static [u8] =
        include_bytes!("../../../../assets/data/icons/pm_dark_red_64.png");
    match get_theme_mode() {
        dark_light::Mode::Light => DARK_RED_ICON,
        _ => LIGHT_RED_ICON,
    }
}

fn get_yellow_icon() -> &'static [u8] {
    const LIGHT_YELLOW_ICON: &'static [u8] =
        include_bytes!("../../../../assets/data/icons/pm_light_yellow_64.png");
    const DARK_YELLOW_ICON: &'static [u8] =
        include_bytes!("../../../../assets/data/icons/pm_dark_yellow_64.png");
    match get_theme_mode() {
        dark_light::Mode::Light => DARK_YELLOW_ICON,
        _ => LIGHT_YELLOW_ICON,
    }
}

fn get_icon(icon: IconColor) -> &'static [u8] {
    match icon {
        IconColor::Red => get_red_icon(),
        IconColor::Green => get_green_icon(),
        IconColor::Blue => get_blue_icon(),
        IconColor::Yellow => get_yellow_icon(),
    }
}

pub fn setup_tray_menu(
    app: &mut tauri::App,
) -> core::result::Result<AppIcon, Box<dyn std::error::Error>> {
    // Tray menu
    load_theme(app.handle());
    let open_btn = MenuItemBuilder::with_id("open", "Open App").build(app)?;
    let exit_ui_btn = MenuItemBuilder::with_id("exit_ui", "Exit UI").build(app)?;
    let shutdown_btn = MenuItemBuilder::with_id("shutdown", "Shut Down Portmaster").build(app)?;

    let global_status = MenuItemBuilder::with_id("global_status", "Status: Secured")
        .enabled(false)
        .build(app)
        .unwrap();
    {
        let mut button_ref = GLOBAL_STATUS.lock()?;
        *button_ref = Some(global_status.clone());
    }

    // Setup SPN status
    let spn_status = MenuItemBuilder::with_id("spn_status", "SPN: Disabled")
        .enabled(false)
        .build(app)
        .unwrap();
    {
        let mut button_ref = SPN_STATUS.lock()?;
        *button_ref = Some(spn_status.clone());
    }

    // Setup SPN button
    let spn = MenuItemBuilder::with_id("spn_toggle", "Enable SPN")
        .build(app)
        .unwrap();
    {
        let mut button_ref = SPN_BUTTON.lock()?;
        *button_ref = Some(spn.clone());
    }

    let system_theme = MenuItemBuilder::with_id("system_theme", "System")
        .build(app)
        .unwrap();
    let light_theme = MenuItemBuilder::with_id("light_theme", "Light")
        .build(app)
        .unwrap();
    let dark_theme = MenuItemBuilder::with_id("dark_theme", "Dark")
        .build(app)
        .unwrap();
    let theme_menu = SubmenuBuilder::new(app, "Icon Theme")
        .items(&[&system_theme, &light_theme, &dark_theme])
        .build()?;

    let force_show_window = MenuItemBuilder::with_id("force-show", "Force Show UI").build(app)?;
    let reload_btn = MenuItemBuilder::with_id("reload", "Reload User Interface").build(app)?;
    let developer_menu = SubmenuBuilder::new(app, "Developer")
        .items(&[&reload_btn, &force_show_window])
        .build()?;

    let menu = MenuBuilder::new(app)
        .items(&[
            &open_btn,
            &PredefinedMenuItem::separator(app)?,
            &global_status,
            &PredefinedMenuItem::separator(app)?,
            &spn_status,
            &spn,
            &PredefinedMenuItem::separator(app)?,
            &theme_menu,
            &PredefinedMenuItem::separator(app)?,
            &exit_ui_btn,
            &shutdown_btn,
            &developer_menu,
        ])
        .build()?;

    let icon = TrayIconBuilder::with_id(PM_TRAY_ICON_ID)
        .icon(Image::from_bytes(get_red_icon()).unwrap())
        .menu(&menu)
        .on_menu_event(move |app, event| match event.id().as_ref() {
            "exit_ui" => {
                let handle = app.clone();
                app.dialog()
                    .message("This does not stop the Portmaster system service")
                    .title("Do you really want to quit the user interface?")
                    .buttons(MessageDialogButtons::OkCancelCustom(
                        "Yes, exit".to_owned(),
                        "No".to_owned(),
                    ))
                    .show(move |answer| {
                        if answer {
                            // let _ = handle.emit("exit-requested", "");
                            handle.exit(0);
                        }
                    });
            }
            "open" => {
                let _ = open_window(app);
            }
            "reload" => {
                if let Ok(mut win) = open_window(app) {
                    may_navigate_to_ui(&mut win, true);
                }
            }
            "force-show" => {
                match create_main_window(app) {
                    Ok(mut win) => {
                        may_navigate_to_ui(&mut win, true);
                        if let Err(err) = win.show() {
                            error!("[tauri] failed to show window: {}", err.to_string());
                        };
                    }
                    Err(err) => {
                        error!("[tauri] failed to create main window: {}", err.to_string());
                    }
                };
            }
            "spn_toggle" => {
                if SPN_STATE.load(Ordering::Acquire) {
                    app.portmaster().set_spn_enabled(false);
                } else {
                    app.portmaster().set_spn_enabled(true);
                }
            }
            "shutdown" => {
                app.portmaster().trigger_shutdown();
            }
            "system_theme" => update_icon_theme(app, dark_light::Mode::Default),
            "dark_theme" => update_icon_theme(app, dark_light::Mode::Dark),
            "light_theme" => update_icon_theme(app, dark_light::Mode::Light),
            other => {
                error!("unknown menu event id: {}", other);
            }
        })
        .on_tray_icon_event(|tray, event| {
            // not supported on linux

            if let tauri::tray::TrayIconEvent::Click {
                id: _,
                position: _,
                rect: _,
                button,
                button_state,
            } = event
            {
                if let MouseButton::Left = button {
                    if let MouseButtonState::Down = button_state {
                        let _ = open_window(tray.app_handle());
                    }
                }
            }
        })
        .build(app)?;
    Ok(icon)
}

pub fn update_icon(icon: AppIcon, subsystems: HashMap<String, Subsystem>, spn_status: String) {
    // iterate over the subsytems and check if there's a module failure
    let failure = subsystems
        .values()
        .into_iter()
        .map(|s| &s.module_status)
        .fold((subsystem::FAILURE_NONE, "".to_string()), |mut acc, s| {
            for m in s {
                if m.failure_status > acc.0 {
                    acc = (m.failure_status, m.failure_msg.clone())
                }
            }
            acc
        });

    if failure.0 == subsystem::FAILURE_NONE {
        if let Some(global_status) = &mut *(GLOBAL_STATUS.lock().unwrap()) {
            _ = global_status.set_text("Status: Secured");
        }
    } else {
        if let Some(global_status) = &mut *(GLOBAL_STATUS.lock().unwrap()) {
            _ = global_status.set_text(format!("Status: {}", failure.1));
        }
    }

    let icon_color = match failure.0 {
        subsystem::FAILURE_WARNING => IconColor::Yellow,
        subsystem::FAILURE_ERROR => IconColor::Red,
        _ => match spn_status.as_str() {
            "connected" | "connecting" => IconColor::Blue,
            _ => IconColor::Green,
        },
    };
    update_icon_color(&icon, icon_color);
}

pub async fn tray_handler(cli: PortAPI, app: tauri::AppHandle) {
    let icon = match app.tray_by_id(PM_TRAY_ICON_ID) {
        Some(icon) => icon,
        None => {
            error!("cancel try_handler: missing try icon");
            return;
        }
    };

    let mut subsystem_subscription = match cli
        .request(Request::QuerySubscribe(
            "query runtime:subsystems/".to_string(),
        ))
        .await
    {
        Ok(rx) => rx,
        Err(err) => {
            error!(
                "cancel try_handler: failed to subscribe to 'runtime:subsystems': {}",
                err
            );
            return;
        }
    };

    let mut spn_status_subscription = match cli
        .request(Request::QuerySubscribe(
            "query runtime:spn/status".to_string(),
        ))
        .await
    {
        Ok(rx) => rx,
        Err(err) => {
            error!(
                "cancel try_handler: failed to subscribe to 'runtime:spn/status': {}",
                err
            );
            return;
        }
    };

    let mut spn_config_subscription = match cli
        .request(Request::QuerySubscribe(
            "query config:spn/enable".to_string(),
        ))
        .await
    {
        Ok(rx) => rx,
        Err(err) => {
            error!(
                "cancel try_handler: failed to subscribe to 'runtime:spn/enable': {}",
                err
            );
            return;
        }
    };

    let mut portmaster_shutdown_event_subscription = match cli
        .request(Request::Subscribe(
            "query runtime:modules/core/event/shutdown".to_string(),
        ))
        .await
    {
        Ok(rx) => rx,
        Err(err) => {
            error!(
                "cancel try_handler: failed to subscribe to 'runtime:modules/core/event/shutdown': {}",
                err
            );
            return;
        }
    };

    update_icon_color(&icon, IconColor::Blue);

    let mut subsystems: HashMap<String, Subsystem> = HashMap::new();
    let mut spn_status: String = "".to_string();

    loop {
        tokio::select! {
            msg = subsystem_subscription.recv() => {
                let msg = match msg {
                    Some(m) => m,
                    None => { break }
                };

                let res = match msg {
                    Response::Ok(key, payload) => Some((key, payload)),
                    Response::New(key, payload) => Some((key, payload)),
                    Response::Update(key, payload) => Some((key, payload)),
                    _ => None,
                };

                if let Some((_, payload)) = res {
                    match payload.parse::<Subsystem>() {
                        Ok(n) => {
                            subsystems.insert(n.id.clone(), n);

                            update_icon(icon.clone(), subsystems.clone(), spn_status.clone());
                        },
                        Err(err) => match err {
                            ParseError::JSON(err) => {
                                error!("failed to parse subsystem: {}", err);
                            }
                            _ => {
                                error!("unknown error when parsing notifications payload");
                            }
                        },
                    }
                }
            },
            msg = spn_status_subscription.recv() => {
                let msg = match msg {
                    Some(m) => m,
                    None => { break }
                };

                let res = match msg {
                    Response::Ok(key, payload) => Some((key, payload)),
                    Response::New(key, payload) => Some((key, payload)),
                    Response::Update(key, payload) => Some((key, payload)),
                    _ => None,
                };

                if let Some((_, payload)) = res {
                    match payload.parse::<SPNStatus>() {
                        Ok(value) => {
                            debug!("SPN status update: {}", value.status);
                            spn_status = value.status.clone();

                            update_icon(icon.clone(), subsystems.clone(), spn_status.clone());
                        },
                        Err(err) => match err {
                            ParseError::JSON(err) => {
                                error!("failed to parse spn status value: {}", err)
                            },
                            _ => {
                                error!("unknown error when parsing spn status value")
                            }
                        }
                    }
                }
            },
            msg = spn_config_subscription.recv() => {
                let msg = match msg {
                    Some(m) => m,
                    None => { break }
                };

                let res = match msg {
                    Response::Ok(key, payload) => Some((key, payload)),
                    Response::New(key, payload) => Some((key, payload)),
                    Response::Update(key, payload) => Some((key, payload)),
                    _ => None,
                };

                if let Some((_, payload)) = res {
                    match payload.parse::<BooleanValue>() {
                        Ok(value) => {
                            update_spn_ui_state(value.value.unwrap_or(false));
                        },
                        Err(err) => match err {
                            ParseError::JSON(err) => {
                                error!("failed to parse config value: {}", err)
                            },
                            _ => {
                                error!("unknown error when parsing config value")
                            }
                        }
                    }
                }
            },
            msg = portmaster_shutdown_event_subscription.recv() => {
                let msg = match msg {
                    Some(m) => m,
                    None => { break }
                };
                debug!("Shutdown request received: {:?}", msg);
                match msg {
                    Response::Ok(msg, _) | Response::New(msg, _) | Response::Update(msg, _) => {
                        if let Err(err) = app.save_window_state(StateFlags::SIZE | StateFlags::POSITION) {
                            error!("failed to save window state: {}", err);
                        }
                        debug!("shutting down: {}", msg);
                        app.exit(0)
                    },
                    _ => {},
                }
            }
        }
    }
    update_spn_ui_state(false);
    update_icon_color(&icon, IconColor::Red);
}

fn update_icon_color(icon: &AppIcon, new_color: IconColor) {
    if let Ok(mut value) = CURRENT_ICON_COLOR.write() {
        *value = new_color;
    }
    _ = icon.set_icon(Some(Image::from_bytes(get_icon(new_color)).unwrap()));
}

fn update_icon_theme(app: &tauri::AppHandle, theme: dark_light::Mode) {
    if let Ok(mut value) = USER_THEME.write() {
        *value = theme;
    }
    let icon = match app.tray_by_id(PM_TRAY_ICON_ID) {
        Some(icon) => icon,
        None => {
            error!("cancel theme update: missing try icon");
            return;
        }
    };
    if let Ok(value) = CURRENT_ICON_COLOR.read() {
        _ = icon.set_icon(Some(Image::from_bytes(get_icon(*value)).unwrap()));
    }
    for (_, v) in app.webview_windows() {
        super::window::set_window_icon(&v);
    }
    save_theme(app, theme);
}

fn load_theme(app: &tauri::AppHandle) {
    match config::load(app) {
        Ok(config) => {
            let theme = match config.theme {
                config::Theme::Light => dark_light::Mode::Light,
                config::Theme::Dark => dark_light::Mode::Dark,
                config::Theme::System => dark_light::Mode::Default,
            };

            if let Ok(mut value) = USER_THEME.write() {
                *value = theme;
            }
        }
        Err(err) => error!("failed to load config file: {}", err),
    }
}

fn save_theme(app: &tauri::AppHandle, mode: dark_light::Mode) {
    match config::load(app) {
        Ok(mut config) => {
            let theme = match mode {
                dark_light::Mode::Dark => config::Theme::Dark,
                dark_light::Mode::Light => config::Theme::Light,
                dark_light::Mode::Default => config::Theme::System,
            };
            config.theme = theme;
            if let Err(err) = config::save(app, config) {
                error!("failed to save config file: {}", err)
            } else {
                debug!("config updated");
            }
        }
        Err(err) => error!("failed to load config file: {}", err),
    }
}

fn update_spn_ui_state(enabled: bool) {
    let mut spn_status = SPN_STATUS.lock().unwrap();
    let Some(spn_status_ref) = &mut *spn_status else {
        return;
    };
    let mut spn_btn = SPN_BUTTON.lock().unwrap();
    let Some(spn_btn_ref) = &mut *spn_btn else {
        return;
    };
    if enabled {
        _ = spn_status_ref.set_text("SPN: Connected");
        _ = spn_btn_ref.set_text("Disable SPN");
    } else {
        _ = spn_status_ref.set_text("SPN: Disabled");
        _ = spn_btn_ref.set_text("Enable SPN");
    }
    SPN_STATE.store(enabled, Ordering::SeqCst);
}
