//! Periodic Prometheus push worker — port of Go `base/metrics` `metricsWriter`
//! and `service/mgr.SleepyTicker`.
//!
//! Every interval the worker renders the registry to Prometheus text and POSTs
//! it to an HTTP(S) endpoint. The Go original used a bare `http.Client{}` with
//! no timeout and returned on the first error; here the request is bounded by a
//! timeout and retried a small, fixed number of times with exponential backoff,
//! and shutdown is cooperative via a [`CancellationToken`].
//!
//! Network access lives behind the [`MetricSender`] trait. The production
//! implementation ([`HttpMetricSender`]) uses `reqwest`; tests inject a fake or
//! point it at a local mock HTTP server, so no privileges or real network are
//! required.

use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::Arc;
use std::time::Duration;

use tokio::sync::Notify;
use tokio_util::sync::CancellationToken;

/// Default push interval, matching Go's `NewSleepyTicker(1*time.Minute, 0)`.
pub const DEFAULT_PUSH_INTERVAL: Duration = Duration::from_secs(60);
/// Overall timeout of a single push request. Go had none; a bounded value keeps
/// a stalled endpoint from blocking shutdown forever.
pub const DEFAULT_PUSH_TIMEOUT: Duration = Duration::from_secs(10);
/// Connect timeout of the built-in HTTP client.
pub const DEFAULT_CONNECT_TIMEOUT: Duration = Duration::from_secs(5);
/// Number of attempts (including the first one) per push cycle.
pub const DEFAULT_MAX_ATTEMPTS: u32 = 3;
/// Prometheus text content type, identical to the `/metrics` API handler.
pub const PUSH_CONTENT_TYPE: &str = "text/plain; version=0.0.4; charset=utf-8";

/// Raw HTTP response of a push, reduced to the fields the worker needs.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PushResponse {
    pub status_code: u16,
    /// Go-style status text (e.g. `503 Service Unavailable`) for error logs.
    pub status: String,
    pub body: Vec<u8>,
}

/// Errors produced by an injected [`MetricSender`].
#[derive(Debug, Clone, PartialEq, Eq, thiserror::Error)]
pub enum PushError {
    #[error("failed to create request: {0}")]
    CreateRequest(String),
    #[error("failed to send metrics: {0}")]
    Send(String),
}

/// Minimal HTTP abstraction used by [`Publisher`].
///
/// Implement this trait to inject a fake in tests; no real network access is
/// then required. The production implementation is [`HttpMetricSender`].
#[async_trait::async_trait]
pub trait MetricSender: Send + Sync {
    /// POSTs `body` as Prometheus text to `url` and returns the raw response.
    async fn send(&self, url: &str, body: &str) -> Result<PushResponse, PushError>;
}

/// Built-in [`MetricSender`] backed by `reqwest` (rustls, bounded timeouts).
pub struct HttpMetricSender {
    client: Result<reqwest::Client, String>,
}

impl Default for HttpMetricSender {
    fn default() -> Self {
        let client = reqwest::Client::builder()
            .timeout(DEFAULT_PUSH_TIMEOUT)
            .connect_timeout(DEFAULT_CONNECT_TIMEOUT)
            .build()
            .map_err(|error| format!("failed to create HTTP client: {error}"));
        Self { client }
    }
}

#[async_trait::async_trait]
impl MetricSender for HttpMetricSender {
    async fn send(&self, url: &str, body: &str) -> Result<PushResponse, PushError> {
        let client = self
            .client
            .as_ref()
            .map_err(|error| PushError::CreateRequest(error.clone()))?;

        let request = client
            .post(url)
            .header(reqwest::header::CONTENT_TYPE, PUSH_CONTENT_TYPE)
            .body(body.to_string())
            .build()
            .map_err(|error| PushError::CreateRequest(error.to_string()))?;

        let response = client
            .execute(request)
            .await
            .map_err(|error| PushError::Send(error.to_string()))?;

        let status_code = response.status().as_u16();
        let reason = response.status().canonical_reason().unwrap_or("");
        let status = if reason.is_empty() {
            status_code.to_string()
        } else {
            format!("{status_code} {reason}")
        };

        // Go ignores a body read failure in its error path, so do the same.
        let response_body = response
            .bytes()
            .await
            .map_or_else(|_| Vec::new(), |bytes| bytes.to_vec());

        Ok(PushResponse {
            status_code,
            status,
            body: response_body,
        })
    }
}

/// Async equivalent of Go's `mgr.SleepyTicker`: a ticker that can be paused
/// ("sleep mode") and woken again. A zero sleep duration means no ticks at all
/// while sleeping, exactly like the Go implementation.
pub struct SleepyTicker {
    normal: Duration,
    sleep: Duration,
    sleeping: AtomicBool,
    changed: Notify,
}

impl SleepyTicker {
    pub fn new(normal: Duration, sleep: Duration) -> Self {
        SleepyTicker {
            normal,
            sleep,
            sleeping: AtomicBool::new(false),
            changed: Notify::new(),
        }
    }

    /// Enables or disables sleep mode, waking any pending wait so the new
    /// interval takes effect immediately (like Go's `ticker.Reset`).
    pub fn set_sleep(&self, enabled: bool) {
        self.sleeping.store(enabled, Ordering::SeqCst);
        self.changed.notify_waiters();
    }

    pub fn is_sleeping(&self) -> bool {
        self.sleeping.load(Ordering::SeqCst)
    }

    /// Waits until the next tick, honouring sleep mode and wake-ups.
    pub async fn tick(&self) {
        loop {
            if self.is_sleeping() && self.sleep.is_zero() {
                // Go returns an unclosed channel here: wait until we wake.
                self.changed.notified().await;
                continue;
            }
            let period = if self.is_sleeping() {
                self.sleep
            } else {
                self.normal
            };
            tokio::select! {
                () = tokio::time::sleep(period) => return,
                () = self.changed.notified() => continue,
            }
        }
    }
}

/// Renders the registry to Prometheus text. Kept as a closure so the worker can
/// be tested without a fully initialised registry.
pub type RenderFn = Arc<dyn Fn() -> String + Send + Sync>;

/// Periodically pushes rendered metrics to a remote endpoint.
pub struct Publisher {
    url: String,
    interval: Duration,
    max_attempts: u32,
    sender: Arc<dyn MetricSender>,
    render: RenderFn,
    ticker: SleepyTicker,
}

impl Publisher {
    /// Creates a publisher with the default interval and retry settings.
    pub fn new(url: impl Into<String>, render: RenderFn, sender: Arc<dyn MetricSender>) -> Self {
        Publisher {
            url: url.into(),
            interval: DEFAULT_PUSH_INTERVAL,
            max_attempts: DEFAULT_MAX_ATTEMPTS,
            sender,
            render,
            ticker: SleepyTicker::new(DEFAULT_PUSH_INTERVAL, Duration::ZERO),
        }
    }

    /// Overrides the push interval (and recreates the ticker).
    pub fn with_interval(mut self, interval: Duration) -> Self {
        self.interval = interval;
        self.ticker = SleepyTicker::new(interval, Duration::ZERO);
        self
    }

    /// Overrides the number of attempts per push cycle (at least one).
    pub fn with_max_attempts(mut self, max_attempts: u32) -> Self {
        self.max_attempts = max_attempts.max(1);
        self
    }

    /// Pauses/resumes ticking, like Go `Metrics.SetSleep`.
    pub fn set_sleep(&self, enabled: bool) {
        self.ticker.set_sleep(enabled);
    }

    /// Returns the configured endpoint (useful for diagnostics).
    pub fn url(&self) -> &str {
        &self.url
    }

    /// Runs the worker until `cancel` is triggered. Shutdown is graceful: an
    /// in-flight request is bounded by the sender timeout and never abandoned.
    pub async fn run(&self, cancel: CancellationToken) {
        loop {
            tokio::select! {
                () = cancel.cancelled() => {
                    tracing::debug!("metrics: push worker stopped");
                    return;
                }
                () = self.ticker.tick() => {
                    if cancel.is_cancelled() {
                        return;
                    }
                    if let Err(error) = self.push_once(&cancel).await {
                        // Go returned from the worker on the first error; the
                        // Rust worker logs and keeps going so a single bad push
                        // does not silently disable metrics forever.
                        tracing::warn!(%error, url = %self.url, "metrics: failed to push metrics");
                    }
                }
            }
        }
    }

    /// Renders and pushes one cycle, retrying transient failures with a bounded
    /// exponential backoff. An empty render is a no-op (Go did the same).
    pub async fn push_once(&self, cancel: &CancellationToken) -> Result<(), PushError> {
        let body = (self.render)();
        if body.is_empty() {
            tracing::debug!("metrics: not pushing metrics, nothing to send");
            return Ok(());
        }

        let mut last_error = None;
        for attempt in 1..=self.max_attempts {
            match self.sender.send(&self.url, &body).await {
                Ok(response) if (200..=299).contains(&response.status_code) => return Ok(()),
                Ok(response) => {
                    let detail = String::from_utf8_lossy(&response.body);
                    last_error = Some(PushError::Send(format!(
                        "got {} while writing metrics to {}: {}",
                        response.status, self.url, detail
                    )));
                }
                Err(error) => last_error = Some(error),
            }

            if attempt < self.max_attempts {
                let backoff = Duration::from_millis(100u64 << (attempt - 1).min(6));
                tokio::select! {
                    () = cancel.cancelled() => return Err(last_error.unwrap_or_else(|| {
                        PushError::Send("push cancelled".to_string())
                    })),
                    () = tokio::time::sleep(backoff) => {}
                }
            }
        }
        Err(last_error.unwrap_or_else(|| PushError::Send("no attempt made".to_string())))
    }

    /// Returns the configured interval.
    pub fn interval(&self) -> Duration {
        self.interval
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::sync::atomic::AtomicUsize;
    use std::sync::Mutex;

    #[derive(Default)]
    struct FakeSender {
        responses: Mutex<Vec<Result<PushResponse, PushError>>>,
        bodies: Mutex<Vec<String>>,
        calls: AtomicUsize,
    }

    #[async_trait::async_trait]
    impl MetricSender for FakeSender {
        async fn send(&self, _url: &str, body: &str) -> Result<PushResponse, PushError> {
            self.bodies.lock().unwrap().push(body.to_string());
            let index = self.calls.fetch_add(1, Ordering::SeqCst);
            self.responses
                .lock()
                .unwrap()
                .get(index)
                .cloned()
                .unwrap_or_else(|| {
                    Ok(PushResponse {
                        status_code: 200,
                        status: "200 OK".to_string(),
                        body: Vec::new(),
                    })
                })
        }
    }

    fn ok(code: u16) -> Result<PushResponse, PushError> {
        Ok(PushResponse {
            status_code: code,
            status: format!("{code} status"),
            body: Vec::new(),
        })
    }

    fn publisher(sender: Arc<FakeSender>) -> Publisher {
        Publisher::new(
            "http://127.0.0.1:1/write",
            Arc::new(|| "portmaster_test 1\n".to_string()),
            sender,
        )
        .with_max_attempts(3)
    }

    #[tokio::test]
    async fn empty_render_is_a_noop() {
        let sender = Arc::new(FakeSender::default());
        let publisher = Publisher::new(
            "http://127.0.0.1:1/write",
            Arc::new(String::new),
            sender.clone(),
        );
        publisher
            .push_once(&CancellationToken::new())
            .await
            .unwrap();
        assert_eq!(sender.calls.load(Ordering::SeqCst), 0);
    }

    #[tokio::test]
    async fn successful_push_sends_rendered_body_once() {
        let sender = Arc::new(FakeSender::default());
        publisher(sender.clone())
            .push_once(&CancellationToken::new())
            .await
            .unwrap();
        assert_eq!(sender.calls.load(Ordering::SeqCst), 1);
        assert_eq!(sender.bodies.lock().unwrap()[0], "portmaster_test 1\n");
    }

    #[tokio::test]
    async fn retries_then_succeeds() {
        let sender = Arc::new(FakeSender::default());
        sender.responses.lock().unwrap().extend([ok(500), ok(503), ok(204)]);
        publisher(sender.clone())
            .push_once(&CancellationToken::new())
            .await
            .unwrap();
        assert_eq!(sender.calls.load(Ordering::SeqCst), 3);
    }

    #[tokio::test]
    async fn gives_up_after_max_attempts() {
        let sender = Arc::new(FakeSender::default());
        sender.responses.lock().unwrap().extend([ok(500), ok(500), ok(500), ok(200)]);
        let error = publisher(sender.clone())
            .push_once(&CancellationToken::new())
            .await
            .unwrap_err();
        assert_eq!(sender.calls.load(Ordering::SeqCst), 3);
        assert!(error.to_string().contains("got 500 status"));
    }

    #[tokio::test]
    async fn worker_stops_on_cancellation() {
        let sender = Arc::new(FakeSender::default());
        let publisher = Arc::new(publisher(sender.clone()).with_interval(Duration::from_millis(5)));
        let cancel = CancellationToken::new();
        let worker_cancel = cancel.clone();
        let task = tokio::spawn(async move { publisher.run(worker_cancel).await });
        tokio::time::sleep(Duration::from_millis(30)).await;
        cancel.cancel();
        tokio::time::timeout(Duration::from_millis(200), task)
            .await
            .expect("worker stopped in time")
            .expect("worker task did not panic");
        assert!(sender.calls.load(Ordering::SeqCst) >= 1);
    }

    #[tokio::test]
    async fn sleepy_ticker_pauses_and_wakes() {
        let ticker = SleepyTicker::new(Duration::from_millis(5), Duration::ZERO);
        ticker.set_sleep(true);
        assert!(ticker.is_sleeping());
        let waiter = tokio::spawn(async move { ticker.tick().await });
        assert!(tokio::time::timeout(Duration::from_millis(30), waiter)
            .await
            .is_err());
    }
}
