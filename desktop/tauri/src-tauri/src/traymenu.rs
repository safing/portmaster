use std::ops::Deref;
use std::sync::atomic::AtomicBool;
use std::sync::RwLock;
use std::{sync::atomic::Ordering};
use chrono::{DateTime, Local};

use log::{debug, error};
use tauri::{
    image::Image,
    menu::{Menu, MenuBuilder, MenuItemBuilder, PredefinedMenuItem, SubmenuBuilder},
    tray::{MouseButton, MouseButtonState, TrayIcon, TrayIconBuilder},
    Manager, Wry,
};
use tauri_plugin_window_state::{AppHandleExt, StateFlags};

use crate::config;
use crate::{
    portapi::{
        client::PortAPI,
        message::{ParseError},
        models::{
            config::BooleanValue,
            spn::SPNStatus,
            system_status_types::{self, SystemStatus},
        },
        types::{Request, Response},
    },
    portmaster::PortmasterExt,
    window::{create_main_window, may_navigate_to_ui, open_window},
};
use tauri_plugin_dialog::{DialogExt, MessageDialogButtons};

pub type AppIcon = TrayIcon<Wry>;
pub type ContextMenu = Menu<Wry>;

static SPN_STATE: AtomicBool = AtomicBool::new(false);

#[derive(Copy, Clone)]
enum IconColor {
    Red,
    Green,
    Blue,
    Yellow,
}

static CURRENT_ICON_COLOR: RwLock<IconColor> = RwLock::new(IconColor::Red);
pub static USER_THEME: RwLock<dark_light::Mode> = RwLock::new(dark_light::Mode::Unspecified);
const OPEN_KEY: &str = "open";
const EXIT_UI_KEY: &str = "exit_ui";
const SPN_STATUS_KEY: &str = "spn_status";
const SPN_BUTTON_KEY: &str = "spn_toggle";
const GLOBAL_STATUS_KEY: &str = "global_status";
const SHUTDOWN_KEY: &str = "shutdown";
const SYSTEM_THEME_KEY: &str = "system_theme";
const LIGHT_THEME_KEY: &str = "light_theme";
const DARK_THEME_KEY: &str = "dark_theme";
const RELOAD_KEY: &str = "reload";
const FORCE_SHOW_KEY: &str = "force-show";

const PM_TRAY_ICON_ID: &str = "pm_icon";
const PM_TRAY_MENU_ID: &str = "pm_tray_menu";

const PAUSE_SPN_5_KEY: &str = "pause_spn_5";
const PAUSE_SPN_15_KEY: &str = "pause_spn_15";
const PAUSE_SPN_60_KEY: &str = "pause_spn_60";
const PAUSE_PM_5_KEY: &str = "pause_pm_5";
const PAUSE_PM_15_KEY: &str = "pause_pm_15";
const PAUSE_PM_60_KEY: &str = "pause_pm_60";
const RESUME_KEY: &str = "resume_all";
const PAUSE_INFO_KEY: &str = "pause_info";
const PAUSE_INFO_TIME_KEY: &str = "pause_info_time";

// Icons

fn get_theme_mode() -> dark_light::Mode {
    if let Ok(value) = USER_THEME.read() {
        return *value.deref();
    }
    dark_light::detect().unwrap_or(dark_light::Mode::Unspecified)
}

fn get_green_icon() -> &'static [u8] {
    const LIGHT_GREEN_ICON: &[u8] =
        include_bytes!("../../../../assets/data/icons/pm_light_green_64.png");
    const DARK_GREEN_ICON: &[u8] =
        include_bytes!("../../../../assets/data/icons/pm_dark_green_64.png");

    match get_theme_mode() {
        dark_light::Mode::Light => DARK_GREEN_ICON,
        _ => LIGHT_GREEN_ICON,
    }
}

fn get_blue_icon() -> &'static [u8] {
    const LIGHT_BLUE_ICON: &[u8] =
        include_bytes!("../../../../assets/data/icons/pm_light_blue_64.png");
    const DARK_BLUE_ICON: &[u8] =
        include_bytes!("../../../../assets/data/icons/pm_dark_blue_64.png");
    match get_theme_mode() {
        dark_light::Mode::Light => DARK_BLUE_ICON,
        _ => LIGHT_BLUE_ICON,
    }
}

fn get_red_icon() -> &'static [u8] {
    const LIGHT_RED_ICON: &[u8] =
        include_bytes!("../../../../assets/data/icons/pm_light_red_64.png");
    const DARK_RED_ICON: &[u8] = include_bytes!("../../../../assets/data/icons/pm_dark_red_64.png");
    match get_theme_mode() {
        dark_light::Mode::Light => DARK_RED_ICON,
        _ => LIGHT_RED_ICON,
    }
}

fn get_yellow_icon() -> &'static [u8] {
    const LIGHT_YELLOW_ICON: &[u8] =
        include_bytes!("../../../../assets/data/icons/pm_light_yellow_64.png");
    const DARK_YELLOW_ICON: &[u8] =
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

fn build_tray_menu(
    app: &tauri::AppHandle,
    status: &str,
    spn_status_text: &str,
    pause_info: &system_status_types::PauseInfo,
) -> core::result::Result<ContextMenu, Box<dyn std::error::Error>> {
    load_theme(app);

    let open_btn = MenuItemBuilder::with_id(OPEN_KEY, "Open App").build(app)?;
    let exit_ui_btn = MenuItemBuilder::with_id(EXIT_UI_KEY, "Exit UI").build(app)?;
    let shutdown_btn = MenuItemBuilder::with_id(SHUTDOWN_KEY, "Shut Down Portmaster").build(app)?;

    // Global status
    let global_status_text = if pause_info.interception {
        format!("Status: {} (PAUSED)", status)
    } else {
        format!("Status: {}", status)
    };
    let global_status = MenuItemBuilder::with_id(GLOBAL_STATUS_KEY, global_status_text)
        .enabled(false)
        .build(app)
        .unwrap();

    // Pause items
    let (pause_status_item, pause_status_time_item, resume_item) = if pause_info.interception || pause_info.spn {
        let status_text = match (pause_info.interception, pause_info.spn) {
            (true, true) => "Portmaster and SPN are paused",
            (true, false) => "Portmaster is paused", 
            (false, true) => "SPN is paused",
            _ => unreachable!(), // We already checked at least one is true
        };
        let status_item = MenuItemBuilder::with_id(PAUSE_INFO_KEY, status_text).enabled(false).build(app).ok();
        
        let time_item = if let Ok(resume_time) = DateTime::parse_from_rfc3339(&pause_info.till_time) {
            let resume_time_local = resume_time.with_timezone(&Local);            
            if resume_time_local > Local::now() {
                let formatted_time = resume_time_local.format("%H:%M:%S").to_string();
                MenuItemBuilder::with_id(PAUSE_INFO_TIME_KEY, format!("Auto-resume at {}", formatted_time)).enabled(false).build(app).ok()
            } else {
                None
            }
        } else {
            None
        };
        
        let resume_item = MenuItemBuilder::with_id(RESUME_KEY, "Resume now").build(app).ok();
        (status_item, time_item, resume_item)
    } else {
        (None, None, None)
    };

    // SPN button    
    let (spn_enabled, spn_button_text ) = match spn_status_text {
        "disabled" => { (false, "Enable SPN") }
        _ => { (true, "Disable SPN") },
    };
    
    let spn_status = MenuItemBuilder::with_id(SPN_STATUS_KEY, format!("SPN: {}", spn_status_text))
        .enabled(false)
        .build(app)
        .unwrap();
    let spn_button = MenuItemBuilder::with_id(SPN_BUTTON_KEY, spn_button_text)
        .build(app)
        .unwrap();

    // Setup Icon theme submenu
    let system_theme = MenuItemBuilder::with_id(SYSTEM_THEME_KEY, "System")
        .build(app)
        .unwrap();
    let light_theme = MenuItemBuilder::with_id(LIGHT_THEME_KEY, "Light")
        .build(app)
        .unwrap();
    let dark_theme = MenuItemBuilder::with_id(DARK_THEME_KEY, "Dark")
        .build(app)
        .unwrap();
    let theme_menu = SubmenuBuilder::new(app, "Icon Theme")
        .items(&[&system_theme, &light_theme, &dark_theme])
        .build()?;


    // Setup Pause/Resume menu items
    let disabled_spn_pause = (!spn_enabled && !pause_info.spn) || pause_info.interception;
    let pause_spn_5min_item = MenuItemBuilder::with_id(PAUSE_SPN_5_KEY, "Pause SPN for 5 minutes").enabled(!disabled_spn_pause).build(app)?;
    let pause_spn_15min_item = MenuItemBuilder::with_id(PAUSE_SPN_15_KEY, "Pause SPN for 15 minutes").enabled(!disabled_spn_pause).build(app)?;
    let pause_spn_1hour_item = MenuItemBuilder::with_id(PAUSE_SPN_60_KEY, "Pause SPN for 1 hour").enabled(!disabled_spn_pause).build(app)?;

    let pause_pm_5min_item = MenuItemBuilder::with_id(PAUSE_PM_5_KEY, "Pause for 5 minutes").build(app)?;
    let pause_pm_15min_item = MenuItemBuilder::with_id(PAUSE_PM_15_KEY, "Pause for 15 minutes").build(app)?;
    let pause_pm_1hour_item = MenuItemBuilder::with_id(PAUSE_PM_60_KEY, "Pause for 1 hour").build(app)?;

    let pause_menu =  if !spn_enabled && !pause_info.spn {
        SubmenuBuilder::new(app, "Pause")
            .items(&[
                &pause_pm_5min_item,
                &pause_pm_15min_item,
                &pause_pm_1hour_item,
            ])
            .build()?
    } else {
        SubmenuBuilder::new(app, "Pause")
            .items(&[
                &pause_spn_5min_item,
                &pause_spn_15min_item,
                &pause_spn_1hour_item,
                &PredefinedMenuItem::separator(app)?,
                &pause_pm_5min_item,
                &pause_pm_15min_item,
                &pause_pm_1hour_item,
            ])
            .build()?
    };

    /* DEV MENU
    let force_show_window = MenuItemBuilder::with_id(FORCE_SHOW_KEY, "Force Show UI").build(app)?;
    let reload_btn = MenuItemBuilder::with_id(RELOAD_KEY, "Reload User Interface").build(app)?;
    let developer_menu = SubmenuBuilder::new(app, "Developer")
        .items(&[&reload_btn, &force_show_window])
        .build()?;
    */
    
    // Assemble menu items
    let s = PredefinedMenuItem::separator(app)?;
    let mut items: Vec<&dyn tauri::menu::IsMenuItem<Wry>> = Vec::new();



    items.push(&global_status);    
    items.push(&s);

    if let Some(ref pause_status_item) = pause_status_item {
        items.push(pause_status_item);
    }
    if let Some(ref pause_status_time_item) = pause_status_time_item {
        items.push(pause_status_time_item);
    }
    if let Some(ref resume_item) = resume_item {
        items.push(resume_item);
    }
    items.push(&pause_menu);
    items.push(&s);

    items.push(&spn_status);
    items.push(&spn_button);
    items.push(&s);

    items.push(&theme_menu);
    items.push(&s);
    
    items.push(&open_btn);
    items.push(&s);

    items.push(&exit_ui_btn);
    items.push(&shutdown_btn);
    //items.push(&developer_menu);
    
    let menu = MenuBuilder::with_id(app, PM_TRAY_MENU_ID)
        .items(&items)
        .build()?;

    return Ok(menu);
}

pub fn setup_tray_menu(
    app: &mut tauri::App,
) -> core::result::Result<AppIcon, Box<dyn std::error::Error>> {
    let menu = build_tray_menu(app.handle(), "unknown", "disabled", &system_status_types::PauseInfo::default())?;

    let icon = TrayIconBuilder::with_id(PM_TRAY_ICON_ID)
        .icon(Image::from_bytes(get_red_icon()).unwrap())
        .menu(&menu)
        .on_menu_event(move |app, event| match event.id().as_ref() {
            EXIT_UI_KEY => {
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
            OPEN_KEY => {
                let _ = open_window(app);
            }
            RELOAD_KEY => {
                if let Ok(mut win) = open_window(app) {
                    may_navigate_to_ui(&mut win, true);
                }
            }
            FORCE_SHOW_KEY => {
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
            SPN_BUTTON_KEY => {
                if SPN_STATE.load(Ordering::Acquire) {
                    app.portmaster().set_spn_enabled(false);
                } else {
                    app.portmaster().set_spn_enabled(true);
                }
            }
            SHUTDOWN_KEY => {
                app.portmaster().trigger_shutdown();
            }
            SYSTEM_THEME_KEY => update_icon_theme(app, dark_light::Mode::Unspecified),
            DARK_THEME_KEY => update_icon_theme(app, dark_light::Mode::Dark),
            LIGHT_THEME_KEY => update_icon_theme(app, dark_light::Mode::Light),

            PAUSE_SPN_5_KEY => app.portmaster().set_pause(60*5, true),
            PAUSE_SPN_15_KEY => app.portmaster().set_pause(60*15, true),
            PAUSE_SPN_60_KEY => app.portmaster().set_pause(60*60, true),
            PAUSE_PM_5_KEY => app.portmaster().set_pause(60*5, false),
            PAUSE_PM_15_KEY => app.portmaster().set_pause(60*15, false),
            PAUSE_PM_60_KEY => app.portmaster().set_pause(60*60, false),
            RESUME_KEY => app.portmaster().set_resume(),

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
                if let (MouseButton::Left, MouseButtonState::Down) = (button, button_state) {
                    let _ = open_window(tray.app_handle());
                }
            }
        })
        .build(app)?;

    Ok(icon)
}

pub fn update_icon(icon: AppIcon, system_status: SystemStatus, spn_status: String) {
    // Extract the worst state type 
    let worst_state_type = system_status.worst_state
        .as_ref()
        .and_then(|ws| ws.state.state_type.clone())
        .unwrap_or(system_status_types::StateType::Undefined);

    // Determine status and icon color in a single match expression
    let (status, icon_color) = match worst_state_type {
        system_status_types::StateType::Error => ("Insecure", IconColor::Red),
        system_status_types::StateType::Warning => ("Insecure", IconColor::Yellow),
        _ => {
            let color = match spn_status.as_str() {
                "connected" | "connecting" => IconColor::Blue,
                _ => IconColor::Green,
            };
            ("Secured", color)
        }
    };

    // Extract pause info from system status
    let pause_info = system_status
        .get_module_state("Control", "control:paused")
        .and_then(|state| state.data.as_ref())
        .and_then(|data| serde_json::from_value::<system_status_types::PauseInfo>(data.clone()).ok())
        .unwrap_or_default();

    // Rebuild and set the tray menu
    if let Ok(menu) = build_tray_menu(icon.app_handle(), status, spn_status.as_str(), &pause_info) {
        if let Err(err) = icon.set_menu(Some(menu)) {
            error!("failed to set menu on tray icon: {}", err.to_string());
        }
    }

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

    let mut system_status_subscription = match cli
        .request(Request::QuerySubscribe(
            "query runtime:system/status".to_string(),
        ))
        .await
    {
        Ok(rx) => rx,
        Err(err) => {
            error!(
                "cancel try_handler: failed to subscribe to 'runtime:system/status': {}",
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

    let mut system_status = SystemStatus::default();
    let mut spn_status: String = "".to_string();

    loop {
        tokio::select! {
            msg = system_status_subscription.recv() => {
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
                    match payload.parse::<SystemStatus>() {
                        Ok(system_status_update) => {
                            system_status.clone_from(&system_status_update);
                            update_icon(icon.clone(), system_status.clone(), spn_status.clone());
                        },
                        Err(err) => match err {
                            ParseError::Json(err) => {
                                error!("failed to parse SystemStatus: {}", err);
                            }
                            _ => {
                                error!("unknown error when parsing SystemStatus payload");
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
                            spn_status.clone_from(&value.status);
                            update_icon(icon.clone(), system_status.clone(), spn_status.clone());
                        },
                        Err(err) => match err {
                            ParseError::Json(err) => {
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
                            SPN_STATE.store(value.value.unwrap_or(false), Ordering::Release);
                        },
                        Err(err) => match err {
                            ParseError::Json(err) => {
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

    update_icon_nostate(icon.clone());
}

pub fn update_icon_nostate(icon: AppIcon) {
    update_icon_color(&icon, IconColor::Red);

    if let Ok(menu) = build_tray_menu(icon.app_handle(), "unknown",  "unknown", &system_status_types::PauseInfo::default()) {
        if let Err(err) = icon.set_menu(Some(menu)) {
            error!("failed to set menu on tray icon: {}", err.to_string());
        }
    }
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
                config::Theme::System => dark_light::Mode::Unspecified,
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
                dark_light::Mode::Unspecified => config::Theme::System,
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
