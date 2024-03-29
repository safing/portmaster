use std::collections::HashMap;
use std::sync::Mutex;

use log::{debug, error};
use tauri::{
    menu::{
        CheckMenuItem, CheckMenuItemBuilder, MenuBuilder, MenuItemBuilder, PredefinedMenuItem,
        SubmenuBuilder,
    },
    tray::{ClickType, TrayIcon, TrayIconBuilder},
    Icon, Manager, Wry,
};
use tauri_plugin_dialog::DialogExt;

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

pub type AppIcon = TrayIcon<Wry>;

lazy_static! {
    // Set once setup_tray_menu executed.
    static ref SPN_BUTTON: Mutex<Option<CheckMenuItem<Wry>>> = Mutex::new(None);
}

// Icons
//
const BLUE_ICON: &'static [u8] =
    include_bytes!("../../assets/icons/pm_light_blue_512.ico");
const RED_ICON: &'static [u8] =
    include_bytes!("../../assets/icons/pm_light_red_512.ico");
const YELLOW_ICON: &'static [u8] =
    include_bytes!("../../assets/icons/pm_light_yellow_512.ico");
const GREEN_ICON: &'static [u8] =
    include_bytes!("../../assets/icons/pm_light_green_512.ico");

pub fn setup_tray_menu(
    app: &mut tauri::App,
) -> core::result::Result<AppIcon, Box<dyn std::error::Error>> {
    // Tray menu
    let close_btn = MenuItemBuilder::with_id("close", "Exit").build(app);
    let open_btn = MenuItemBuilder::with_id("open", "Open").build(app);

    let spn = CheckMenuItemBuilder::with_id("spn", "Use SPN").build(app);

    // Store the SPN button reference
    let mut button_ref = SPN_BUTTON.lock().unwrap();
    *button_ref = Some(spn.clone());

    let force_show_window = MenuItemBuilder::with_id("force-show", "Force Show UI").build(app);
    let reload_btn = MenuItemBuilder::with_id("reload", "Reload User Interface").build(app);
    let developer_menu = SubmenuBuilder::new(app, "Developer")
        .items(&[&reload_btn, &force_show_window])
        .build()?;

    // Drop the reference now so we unlock immediately.
    drop(button_ref);

    let menu = MenuBuilder::new(app)
        .items(&[
            &spn,
            &PredefinedMenuItem::separator(app),
            &open_btn,
            &close_btn,
            &developer_menu,
        ])
        .build()?;

    let icon = TrayIconBuilder::new()
        .icon(Icon::Raw(RED_ICON.to_vec()))
        .menu(&menu)
        .on_menu_event(move |app, event| match event.id().as_ref() {
            "close" => {
                let handle = app.clone();
                app.dialog()
                    .message("This does not stop the Portmaster system service")
                    .title("Do you really want to quit the user interface?")
                    .ok_button_label("Yes, exit")
                    .cancel_button_label("No")
                    .show(move |answer| {
                        if answer {
                            let _ = handle.emit("exit-requested", "");
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
            "spn" => {
                let btn = SPN_BUTTON.lock().unwrap();

                if let Some(bt) = &*btn {
                    if let Ok(is_checked) = bt.is_checked() {
                        app.portmaster().set_spn_enabled(is_checked);
                    }
                }
            }
            other => {
                error!("unknown menu event id: {}", other);
            }
        })
        .on_tray_icon_event(|tray, event| {
            // not supported on linux
            if event.click_type == ClickType::Left {
                let _ = open_window(tray.app_handle());
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
        .map(|s| s.failure_status)
        .fold(
            subsystem::FAILURE_NONE,
            |acc, s| {
                if s > acc {
                    s
                } else {
                    acc
                }
            },
        );

    let next_icon = match failure {
        subsystem::FAILURE_WARNING => YELLOW_ICON,
        subsystem::FAILURE_ERROR => RED_ICON,
        _ => match spn_status.as_str() {
            "connected" | "connecting" => BLUE_ICON,
            _ => GREEN_ICON,
        },
    };

    _ = icon.set_icon(Some(Icon::Raw(next_icon.to_vec())));
}

pub async fn tray_handler(cli: PortAPI, app: tauri::AppHandle) {
    let icon = match app.tray() {
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

    _ = icon.set_icon(Some(Icon::Raw(BLUE_ICON.to_vec())));

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
                            let mut btn = SPN_BUTTON.lock().unwrap();

                            if let Some(btn) = &mut *btn {
                                if let Some(value) = value.value {
                                    _ = btn.set_checked(value);
                                } else {
                                    _ = btn.set_checked(false);
                                }
                            }
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
            }
        }
    }

    if let Some(btn) = &mut *(SPN_BUTTON.lock().unwrap()) {
        _ = btn.set_checked(false);
    }

    _ = icon.set_icon(Some(Icon::Raw(RED_ICON.to_vec())));
}
