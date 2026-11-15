use std::panic::{catch_unwind, AssertUnwindSafe};
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::{Arc, Mutex};

use tokio::sync::mpsc;

/// Result type conversion for event callbacks.
pub trait CallbackResult {
    /// Converts into a boolean indicating whether to cancel/remove the callback.
    fn into_result(self) -> anyhow::Result<bool>;
}

impl CallbackResult for anyhow::Result<bool> {
    fn into_result(self) -> anyhow::Result<bool> {
        self
    }
}

impl CallbackResult for bool {
    fn into_result(self) -> anyhow::Result<bool> {
        Ok(self)
    }
}

struct SubscriptionEntry<T> {
    name: String,
    sender: mpsc::Sender<T>,
}

type EventCallbackFn<T> = dyn Fn(&T) -> anyhow::Result<bool> + Send + Sync;
type EventCallbackBox<T> = Box<EventCallbackFn<T>>;

struct CallbackEntry<T> {
    name: String,
    callback: EventCallbackBox<T>,
    canceled: AtomicBool,
}

struct EventMgrInner<T> {
    name: String,
    subs: Vec<SubscriptionEntry<T>>,
    callbacks: Vec<Arc<CallbackEntry<T>>>,
}

/// Pub-sub event manager supporting channel subscriptions and callbacks.
pub struct EventMgr<T> {
    inner: Arc<Mutex<EventMgrInner<T>>>,
}

impl<T> Clone for EventMgr<T> {
    fn clone(&self) -> Self {
        Self {
            inner: Arc::clone(&self.inner),
        }
    }
}

impl<T> EventMgr<T> {
    /// Creates a new event manager with the specified name.
    pub fn new(name: impl Into<String>) -> Self {
        Self {
            inner: Arc::new(Mutex::new(EventMgrInner {
                name: name.into(),
                subs: Vec::new(),
                callbacks: Vec::new(),
            })),
        }
    }

    /// Returns the name of the event manager.
    pub fn name(&self) -> String {
        let inner = self.inner.lock().unwrap_or_else(|e| e.into_inner());
        inner.name.clone()
    }

    /// Subscribes to events with a receiver channel of the given capacity.
    pub fn subscribe(&self, name: impl Into<String>, capacity: usize) -> mpsc::Receiver<T> {
        let cap = capacity.max(1);
        let (sender, receiver) = mpsc::channel(cap);

        let mut inner = self.inner.lock().unwrap_or_else(|e| e.into_inner());
        inner.subs.push(SubscriptionEntry {
            name: name.into(),
            sender,
        });

        receiver
    }

    /// Adds a callback function that will be executed for every submitted event.
    ///
    /// If `f` returns `Ok(true)` (cancel = true), the callback is removed.
    pub fn add_callback<F>(&self, name: impl Into<String>, f: F)
    where
        F: Fn(T) -> anyhow::Result<bool> + Send + Sync + 'static,
        T: Clone,
    {
        let cb = Box::new(move |event: &T| f(event.clone()));
        let entry = Arc::new(CallbackEntry {
            name: name.into(),
            callback: cb,
            canceled: AtomicBool::new(false),
        });

        let mut inner = self.inner.lock().unwrap_or_else(|e| e.into_inner());
        inner.callbacks.push(entry);
    }

    /// Adds a callback function receiving a reference to the event.
    ///
    /// If `f` returns `Ok(true)` (cancel = true), the callback is removed.
    pub fn add_callback_ref<F>(&self, name: impl Into<String>, f: F)
    where
        F: Fn(&T) -> anyhow::Result<bool> + Send + Sync + 'static,
    {
        let cb = Box::new(move |event: &T| f(event));
        let entry = Arc::new(CallbackEntry {
            name: name.into(),
            callback: cb,
            canceled: AtomicBool::new(false),
        });

        let mut inner = self.inner.lock().unwrap_or_else(|e| e.into_inner());
        inner.callbacks.push(entry);
    }

    /// Submits an event to all active subscribers and callbacks.
    pub fn submit(&self, event: T)
    where
        T: Clone + Send,
    {
        let callbacks = {
            let mut inner = self.inner.lock().unwrap_or_else(|e| e.into_inner());

            let event_name = inner.name.clone();

            // Deliver to subscriptions, pruning closed channels
            inner.subs.retain(|sub| {
                match sub.sender.try_send(event.clone()) {
                    Ok(()) => true,
                    Err(mpsc::error::TrySendError::Full(_)) => {
                        tracing::warn!(
                            event = %event_name,
                            subscriber = %sub.name,
                            "event subscription channel overflow"
                        );
                        true
                    }
                    Err(mpsc::error::TrySendError::Closed(_)) => false,
                }
            });

            inner.callbacks.clone()
        };

        let mut any_canceled = false;
        for cb in callbacks {
            if cb.canceled.load(Ordering::Relaxed) {
                any_canceled = true;
                continue;
            }

            let res = catch_unwind(AssertUnwindSafe(|| (cb.callback)(&event)));

            match res {
                Ok(Ok(cancel)) => {
                    if cancel {
                        cb.canceled.store(true, Ordering::Relaxed);
                        any_canceled = true;
                    }
                }
                Ok(Err(err)) => {
                    tracing::error!(
                        event = %self.name(),
                        callback = %cb.name,
                        error = %err,
                        "event callback returned error"
                    );
                }
                Err(panic_err) => {
                    let msg = if let Some(s) = panic_err.downcast_ref::<&str>() {
                        (*s).to_string()
                    } else if let Some(s) = panic_err.downcast_ref::<String>() {
                        s.clone()
                    } else {
                        "unknown panic".to_string()
                    };
                    tracing::error!(
                        event = %self.name(),
                        callback = %cb.name,
                        panic = %msg,
                        "event callback panicked"
                    );
                }
            }
        }

        if any_canceled {
            let mut inner = self.inner.lock().unwrap_or_else(|e| e.into_inner());
            inner.callbacks.retain(|cb| !cb.canceled.load(Ordering::Relaxed));
        }
    }

    /// Returns the number of registered subscribers.
    pub fn subscriber_count(&self) -> usize {
        let inner = self.inner.lock().unwrap_or_else(|e| e.into_inner());
        inner.subs.len()
    }

    /// Returns the number of active registered callbacks.
    pub fn callback_count(&self) -> usize {
        let inner = self.inner.lock().unwrap_or_else(|e| e.into_inner());
        inner
            .callbacks
            .iter()
            .filter(|cb| !cb.canceled.load(Ordering::Relaxed))
            .count()
    }
}
