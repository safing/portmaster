use crate::portapi::client::*;
use crate::portapi::message::*;
use crate::portapi::models::notification::*;
use crate::portapi::types::*;
use log::error;
use notify_rust;
use serde_json::json;
#[allow(unused_imports)]
use tauri::async_runtime;

pub async fn notification_handler(cli: PortAPI) {
    let res = cli
        .request(Request::QuerySubscribe("query notifications:".to_string()))
        .await;

    if let Ok(mut rx) = res {
        while let Some(msg) = rx.recv().await {
            let res = match msg {
                Response::Ok(key, payload) => Some((key, payload)),
                Response::New(key, payload) => Some((key, payload)),
                Response::Update(key, payload) => Some((key, payload)),
                _ => None,
            };

            if let Some((key, payload)) = res {
                match payload.parse::<Notification>() {
                    Ok(n) => {
                        // Skip if this one should not be shown using the system notifications
                        if !n.show_on_system {
                            return;
                        }

                        // Skip if this action has already been acted on
                        if n.selected_action_id != "" {
                            return;
                        }

                        // TODO(ppacher): keep a reference of open notifications and close them
                        // if the user reacted inside the UI:

                        let mut notif = notify_rust::Notification::new();
                        notif.body(&n.message);
                        notif.timeout(notify_rust::Timeout::Never); // TODO(ppacher): use n.expires to calculate the timeout.
                        notif.summary(&n.title);
                        notif.icon("portmaster");

                        for action in n.actions {
                            notif.action(&action.id, &action.text);
                        }

                        #[cfg(target_os = "linux")]
                        {
                            let cli_clone = cli.clone();
                            async_runtime::spawn(async move {
                                let res = notif.show();
                                match res {
                                    Ok(handle) => {
                                        handle.wait_for_action(|action| {
                                            match action {
                                                "__closed" => {
                                                    // timeout
                                                }

                                                value => {
                                                    let value = value.to_string().clone();

                                                    async_runtime::spawn(async move {
                                                        let _ = cli_clone
                                                            .request(Request::Update(
                                                                key,
                                                                Payload::JSON(
                                                                    json!({
                                                                        "SelectedActionID": value
                                                                    })
                                                                    .to_string(),
                                                                ),
                                                            ))
                                                            .await;
                                                    });
                                                }
                                            }
                                        })
                                    }
                                    Err(err) => {
                                        error!("failed to display notification: {}", err);
                                    }
                                }
                            });
                        }
                    }
                    Err(err) => match err {
                        ParseError::JSON(err) => {
                            error!("failed to parse notification: {}", err);
                        }
                        _ => {
                            error!("unknown error when parsing notifications payload");
                        }
                    },
                }
            }
        }
    }
}
