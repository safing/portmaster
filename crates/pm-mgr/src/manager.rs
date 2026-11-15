use std::any::Any;
use std::future::Future;
use std::sync::{Arc, Mutex};
use std::time::Duration;

use tokio::sync::Notify;
use tokio::task::{AbortHandle, JoinHandle};
use tokio::time::{interval_at, Instant, MissedTickBehavior};
use tokio_util::sync::CancellationToken;

/// Conversion trait for worker return types.
pub trait WorkerResult: Send {
    /// Converts the result into an anyhow::Result.
    fn into_result(self) -> anyhow::Result<()>;
}

impl WorkerResult for () {
    fn into_result(self) -> anyhow::Result<()> {
        Ok(())
    }
}

impl WorkerResult for anyhow::Result<()> {
    fn into_result(self) -> anyhow::Result<()> {
        self
    }
}

struct ManagerInner {
    name: String,
    cancel_token: CancellationToken,
    abort_handles: Vec<AbortHandle>,
    worker_count: usize,
}

struct WorkerGuard {
    inner: Arc<Mutex<ManagerInner>>,
    notify: Arc<Notify>,
}

impl Drop for WorkerGuard {
    fn drop(&mut self) {
        let mut inner = self.inner.lock().unwrap_or_else(|e| e.into_inner());
        inner.worker_count = inner.worker_count.saturating_sub(1);
        drop(inner);
        self.notify.notify_waiters();
    }
}

/// Module manager that oversees background workers, task scheduling,
/// cancellation, and panic recovery.
#[derive(Clone)]
pub struct Manager {
    inner: Arc<Mutex<ManagerInner>>,
    notify: Arc<Notify>,
}

impl Manager {
    /// Creates a new manager with the given name.
    pub fn new(name: impl Into<String>) -> Self {
        Self {
            inner: Arc::new(Mutex::new(ManagerInner {
                name: name.into(),
                cancel_token: CancellationToken::new(),
                abort_handles: Vec::new(),
                worker_count: 0,
            })),
            notify: Arc::new(Notify::new()),
        }
    }

    /// Creates a new manager with a specified cancellation token.
    pub fn with_cancellation_token(
        name: impl Into<String>,
        cancel_token: CancellationToken,
    ) -> Self {
        Self {
            inner: Arc::new(Mutex::new(ManagerInner {
                name: name.into(),
                cancel_token,
                abort_handles: Vec::new(),
                worker_count: 0,
            })),
            notify: Arc::new(Notify::new()),
        }
    }

    /// Returns the manager name.
    pub fn name(&self) -> String {
        let inner = self.inner.lock().unwrap_or_else(|e| e.into_inner());
        inner.name.clone()
    }

    /// Returns the cancellation token associated with this manager.
    pub fn cancel_token(&self) -> CancellationToken {
        let inner = self.inner.lock().unwrap_or_else(|e| e.into_inner());
        inner.cancel_token.clone()
    }

    /// Alias for `cancel_token`.
    pub fn cancellation_token(&self) -> CancellationToken {
        self.cancel_token()
    }

    /// Cancels this manager's cancellation token.
    pub fn cancel(&self) {
        let inner = self.inner.lock().unwrap_or_else(|e| e.into_inner());
        inner.cancel_token.cancel();
    }

    /// Returns true if the manager context has been cancelled.
    pub fn is_cancelled(&self) -> bool {
        let inner = self.inner.lock().unwrap_or_else(|e| e.into_inner());
        inner.cancel_token.is_cancelled()
    }

    /// Returns the current active worker count.
    pub fn worker_count(&self) -> usize {
        let inner = self.inner.lock().unwrap_or_else(|e| e.into_inner());
        inner.worker_count
    }

    /// Runs a future as a monitored background worker.
    ///
    /// If the worker returns an error or panics, it is caught and logged
    /// via `tracing::error`, without crashing the process.
    pub fn go<F, R>(&self, name: impl Into<String>, fut: F) -> JoinHandle<()>
    where
        F: Future<Output = R> + Send + 'static,
        R: WorkerResult + 'static,
    {
        let worker_name = name.into();
        let supervisor_name = worker_name.clone();

        let guard = WorkerGuard {
            inner: Arc::clone(&self.inner),
            notify: Arc::clone(&self.notify),
        };

        let worker_task = tokio::spawn(async move {
            fut.await.into_result()
        });

        let worker_abort = worker_task.abort_handle();

        let supervisor = tokio::spawn(async move {
            let _guard = guard;
            match worker_task.await {
                Ok(Ok(())) => {
                    tracing::debug!(worker = %supervisor_name, "worker finished");
                }
                Ok(Err(err)) => {
                    tracing::error!(
                        worker = %supervisor_name,
                        error = %err,
                        "worker returned error"
                    );
                }
                Err(join_err) => {
                    if join_err.is_panic() {
                        let panic = join_err.into_panic();
                        let msg = format_panic(&*panic);
                        tracing::error!(
                            worker = %supervisor_name,
                            panic = %msg,
                            "worker panicked"
                        );
                    } else {
                        tracing::debug!(
                            worker = %supervisor_name,
                            "worker cancelled or aborted"
                        );
                    }
                }
            }
        });

        self.register_worker(&[worker_abort, supervisor.abort_handle()]);
        supervisor
    }

    /// Periodically executes `f` every `interval` until cancelled.
    ///
    /// Worker panics and errors in individual iterations are caught and logged.
    pub fn repeat<F, Fut, R>(
        &self,
        name: impl Into<String>,
        interval: Duration,
        mut f: F,
    ) -> JoinHandle<()>
    where
        F: FnMut() -> Fut + Send + 'static,
        Fut: Future<Output = R> + Send + 'static,
        R: WorkerResult + 'static,
    {
        let worker_name = name.into();
        let cancel_token = self.cancel_token();
        let supervisor_name = worker_name.clone();

        let guard = WorkerGuard {
            inner: Arc::clone(&self.inner),
            notify: Arc::clone(&self.notify),
        };

        let worker_task = tokio::spawn(async move {
            let mut ticker = interval_at(Instant::now() + interval, interval);
            ticker.set_missed_tick_behavior(MissedTickBehavior::Delay);

            loop {
                tokio::select! {
                    _ = cancel_token.cancelled() => {
                        tracing::debug!(worker = %worker_name, "repeat worker received cancellation");
                        break;
                    }
                    _ = ticker.tick() => {
                        if cancel_token.is_cancelled() {
                            break;
                        }

                        let fut = f();
                        let iter_name = worker_name.clone();
                        let iter_task = tokio::spawn(async move {
                            fut.await.into_result()
                        });

                        match iter_task.await {
                            Ok(Ok(())) => {}
                            Ok(Err(err)) => {
                                tracing::error!(
                                    worker = %iter_name,
                                    error = %err,
                                    "repeat worker iteration error"
                                );
                            }
                            Err(join_err) if join_err.is_panic() => {
                                let panic = join_err.into_panic();
                                let msg = format_panic(&*panic);
                                tracing::error!(
                                    worker = %iter_name,
                                    panic = %msg,
                                    "repeat worker iteration panicked"
                                );
                            }
                            Err(_) => {}
                        }
                    }
                }
            }
            Ok::<(), anyhow::Error>(())
        });

        let worker_abort = worker_task.abort_handle();

        let supervisor = tokio::spawn(async move {
            let _guard = guard;
            if let Err(join_err) = worker_task.await {
                if join_err.is_panic() {
                    let panic = join_err.into_panic();
                    let msg = format_panic(&*panic);
                    tracing::error!(
                        worker = %supervisor_name,
                        panic = %msg,
                        "repeat worker panicked"
                    );
                }
            }
        });

        self.register_worker(&[worker_abort, supervisor.abort_handle()]);
        supervisor
    }

    /// Schedules `f` to run once after `delay` has elapsed.
    ///
    /// If the manager is cancelled before the delay elapses, `f` is not executed.
    pub fn delay<F, Fut, R>(
        &self,
        name: impl Into<String>,
        delay: Duration,
        f: F,
    ) -> JoinHandle<()>
    where
        F: FnOnce() -> Fut + Send + 'static,
        Fut: Future<Output = R> + Send + 'static,
        R: WorkerResult + 'static,
    {
        let worker_name = name.into();
        let cancel_token = self.cancel_token();
        let supervisor_name = worker_name.clone();

        let guard = WorkerGuard {
            inner: Arc::clone(&self.inner),
            notify: Arc::clone(&self.notify),
        };

        let worker_task = tokio::spawn(async move {
            tokio::select! {
                _ = cancel_token.cancelled() => {
                    tracing::debug!(worker = %worker_name, "delayed worker cancelled before execution");
                    Ok::<(), anyhow::Error>(())
                }
                _ = tokio::time::sleep(delay) => {
                    if cancel_token.is_cancelled() {
                        return Ok::<(), anyhow::Error>(());
                    }
                    let fut = f();
                    fut.await.into_result()
                }
            }
        });

        let worker_abort = worker_task.abort_handle();

        let supervisor = tokio::spawn(async move {
            let _guard = guard;
            match worker_task.await {
                Ok(Ok(())) => {}
                Ok(Err(err)) => {
                    tracing::error!(
                        worker = %supervisor_name,
                        error = %err,
                        "delayed worker error"
                    );
                }
                Err(join_err) => {
                    if join_err.is_panic() {
                        let panic = join_err.into_panic();
                        let msg = format_panic(&*panic);
                        tracing::error!(
                            worker = %supervisor_name,
                            panic = %msg,
                            "delayed worker panicked"
                        );
                    }
                }
            }
        });

        self.register_worker(&[worker_abort, supervisor.abort_handle()]);
        supervisor
    }

    /// Waits for all registered workers to complete, up to an optional timeout.
    ///
    /// If timeout is exceeded, lingering workers are aborted.
    pub async fn wait_for_workers(&self, timeout: Option<Duration>) {
        let notify = Arc::clone(&self.notify);
        let wait_fut = async {
            while self.worker_count() > 0 {
                notify.notified().await;
            }
        };

        if let Some(dur) = timeout {
            if tokio::time::timeout(dur, wait_fut).await.is_err() {
                tracing::warn!(
                    manager = %self.name(),
                    "timed out waiting for workers to stop; aborting remaining"
                );
                self.abort_all_workers();
            }
        } else {
            wait_fut.await;
        }
    }

    /// Resets the manager for restart by cancelling current context,
    /// generating a new cancellation token, and clearing workers.
    pub fn reset(&self) {
        self.cancel();
        self.abort_all_workers();
        let mut inner = self.inner.lock().unwrap_or_else(|e| e.into_inner());
        inner.cancel_token = CancellationToken::new();
        inner.worker_count = 0;
    }

    fn register_worker(&self, abort_handles: &[AbortHandle]) {
        let mut inner = self.inner.lock().unwrap_or_else(|e| e.into_inner());
        inner.worker_count += 1;
        inner.abort_handles.retain(|h| !h.is_finished());
        for h in abort_handles {
            inner.abort_handles.push(h.clone());
        }
    }

    fn abort_all_workers(&self) {
        let mut inner = self.inner.lock().unwrap_or_else(|e| e.into_inner());
        for handle in inner.abort_handles.drain(..) {
            handle.abort();
        }
    }
}

fn format_panic(panic: &(dyn Any + Send + 'static)) -> String {
    if let Some(s) = panic.downcast_ref::<&str>() {
        (*s).to_string()
    } else if let Some(s) = panic.downcast_ref::<String>() {
        s.clone()
    } else {
        "unknown panic".to_string()
    }
}
