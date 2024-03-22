use futures_util::{SinkExt, StreamExt};
use http::Uri;
use log::{debug, error, warn};
use std::collections::HashMap;
use std::sync::atomic::{AtomicUsize, Ordering};
use tokio::sync::mpsc::{channel, Receiver, Sender};
use tokio::sync::RwLock;
use tokio_websockets::{ClientBuilder, Error};

use super::message::*;
use super::types::*;

/// An internal representation of a Command that
/// contains the PortAPI message as well as a response
/// channel that will receive all responses sent from the
/// server.
///
/// Users should normally not need to use the Command struct
/// directly since `PortAPI` already abstracts the creation of
/// mpsc channels.
struct Command {
    msg: Message,
    response: Sender<Response>,
}

/// The client implementation for PortAPI.
#[derive(Clone)]
pub struct PortAPI {
    dispatch: Sender<Command>,
}

/// The map type used to store message subscribers.
type SubscriberMap = RwLock<HashMap<usize, Sender<Response>>>;

/// Connect to PortAPI at the specified URI.
///
/// This method will launch a new async thread on the `tauri::async_runtime`
/// that will handle message to transmit and also multiplex server responses
/// to the appropriate subscriber.
pub async fn connect(uri: &str) -> Result<PortAPI, Error> {
    let parsed = match uri.parse::<Uri>() {
        Ok(u) => u,
        Err(_e) => {
            return Err(Error::NoUriConfigured); // TODO(ppacher): fix the return error type.
        }
    };

    let (mut client, _) = ClientBuilder::from_uri(parsed).connect().await?;
    let (tx, mut dispatch) = channel::<Command>(64);

    tauri::async_runtime::spawn(async move {
        let subscribers: SubscriberMap = RwLock::new(HashMap::new());
        let next_id = AtomicUsize::new(0);

        loop {
            tokio::select! {
                msg = client.next() => {
                    let msg = match msg {
                        Some(msg) => msg,
                        None => {
                            warn!("websocket connection lost");

                            dispatch.close();
                            return;
                        }
                    };

                    match msg {
                        Err(err) => {
                            error!("failed to receive frame from websocket: {}", err);

                            dispatch.close();
                            return;
                        },
                        Ok(msg) => {
                            let text = unsafe {
                                std::str::from_utf8_unchecked(msg.as_payload())
                            };

                            match text.parse::<Message>() {
                                Ok(msg) => {
                                    let id = msg.id;
                                    let map = subscribers
                                        .read()
                                        .await;

                                    if let Some(sub) = map.get(&id) {
                                        let res: Result<Response, MessageError> = msg.try_into();
                                        match res {
                                            Ok(response) => {
                                                if let Err(err) = sub.send(response).await {
                                                    // The receiver side has been closed already,
                                                    // drop the read lock and remove the subscriber
                                                    // from our hashmap
                                                    drop(map);

                                                    subscribers
                                                        .write()
                                                        .await
                                                        .remove(&id);

                                                    debug!("subscriber for command {} closed read side: {}", id, err);
                                                }
                                            },
                                            Err(err) => {
                                                error!("invalid command: {}", err);
                                            }
                                        }
                                    }
                                },
                                Err(err) => {
                                    error!("failed to deserialize message: {}", err)
                                }
                            }
                        }
                    }

                },

                Some(mut cmd) = dispatch.recv() => {
                    let id = next_id.fetch_add(1, Ordering::Relaxed);
                    cmd.msg.id = id;
                    let blob: String = cmd.msg.into();

                    debug!("Sending websocket frame: {}", blob);

                    match client.send(tokio_websockets::Message::text(blob)).await {
                        Ok(_) => {
                            subscribers
                                .write()
                                .await
                                .insert(id, cmd.response);
                        },
                        Err(err) => {
                            error!("failed to dispatch command: {}", err);

                            // TODO(ppacher): we should send some error to cmd.response here.
                            // Otherwise, the sender of cmd might get stuck waiting for responses
                            // if they don't check for PortAPI.is_closed().

                            return
                        }
                    }
                }
            }
        }
    });

    Ok(PortAPI { dispatch: tx })
}

impl PortAPI {
    /// `request` sends a PortAPI `portapi::types::Request` to the server and returns a mpsc receiver channel
    /// where all server responses are forwarded.
    ///
    /// If the caller does not intend to read any responses the returned receiver may be closed or
    /// dropped. As soon as the async-thread launched in `connect` detects a closed receiver it is remove
    /// from the subscription map.
    ///
    /// The default buffer size for the channel is 64. Use `request_with_buffer_size` to specify a dedicated buffer size.
    pub async fn request(
        &self,
        r: Request,
    ) -> std::result::Result<Receiver<Response>, MessageError> {
        self.request_with_buffer_size(r, 64).await
    }

    // Like `request` but supports explicitly specifying a channel buffer size.
    pub async fn request_with_buffer_size(
        &self,
        r: Request,
        buffer: usize,
    ) -> std::result::Result<Receiver<Response>, MessageError> {
        let (tx, rx) = channel(buffer);

        let msg: Message = r.try_into()?;

        let _ = self.dispatch.send(Command { response: tx, msg }).await;

        Ok(rx)
    }

    /// Reports whether or not the websocket connection to the Portmaster Database API has been closed
    /// due to errors.
    ///
    /// Users are expected to check this field on a regular interval to detect any issues and perform
    /// a clean re-connect by calling `connect` again.
    pub fn is_closed(&self) -> bool {
        self.dispatch.is_closed()
    }
}
