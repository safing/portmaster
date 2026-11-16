//! Publisher tests against a real (local) mock HTTP server using the built-in
//! reqwest sender. No root and no external network are needed.

use std::sync::Arc;
use std::time::Duration;

use pm_metrics::push::{HttpMetricSender, Publisher};
use tokio::io::{AsyncReadExt, AsyncWriteExt};
use tokio::net::TcpListener;
use tokio_util::sync::CancellationToken;

/// Reads one HTTP/1.1 POST request (headers + Content-Length body) and replies
/// with the given status line. Returns the request body.
async fn serve_one(listener: TcpListener, status: &'static str) -> String {
    let (mut socket, _) = listener.accept().await.expect("client connected");
    let mut buffer = Vec::new();
    let header_end = loop {
        let mut chunk = [0u8; 1024];
        let count = socket.read(&mut chunk).await.expect("read request");
        if count == 0 {
            panic!("client closed before headers");
        }
        buffer.extend_from_slice(&chunk[..count]);
        if let Some(pos) = buffer.windows(4).position(|w| w == b"\r\n\r\n") {
            break pos + 4;
        }
    };

    let headers = String::from_utf8_lossy(&buffer[..header_end]).to_string();
    assert!(headers.starts_with("POST "), "expected POST: {headers}");
    let content_length = headers
        .lines()
        .find_map(|line| {
            let (name, value) = line.split_once(':')?;
            if name.eq_ignore_ascii_case("content-length") {
                value.trim().parse::<usize>().ok()
            } else {
                None
            }
        })
        .expect("Content-Length header");

    while buffer.len() < header_end + content_length {
        let mut chunk = [0u8; 1024];
        let count = socket.read(&mut chunk).await.expect("read body");
        if count == 0 {
            break;
        }
        buffer.extend_from_slice(&chunk[..count]);
    }
    let body = String::from_utf8_lossy(&buffer[header_end..header_end + content_length]).to_string();

    socket
        .write_all(
            format!("HTTP/1.1 {status}\r\nContent-Length: 0\r\nConnection: close\r\n\r\n").as_bytes(),
        )
        .await
        .expect("write response");
    socket.flush().await.expect("flush response");
    body
}

#[tokio::test]
async fn pushes_prometheus_text_to_mock_server() {
    let listener = TcpListener::bind("127.0.0.1:0").await.expect("bind");
    let address = listener.local_addr().expect("local addr");
    let server = tokio::spawn(serve_one(listener, "200 OK"));

    let body = "portmaster_test_metric{instance=\"ci\"} 42\n".to_string();
    let render: pm_metrics::push::RenderFn = Arc::new(move || body.clone());
    let publisher = Publisher::new(
        format!("http://{address}/api/v1/write"),
        render,
        Arc::new(HttpMetricSender::default()),
    );

    publisher
        .push_once(&CancellationToken::new())
        .await
        .expect("push succeeded");

    let received = tokio::time::timeout(Duration::from_secs(2), server)
        .await
        .expect("server responded in time")
        .expect("server task");
    assert_eq!(received, "portmaster_test_metric{instance=\"ci\"} 42\n");
}

#[tokio::test]
async fn non_success_status_is_reported() {
    let listener = TcpListener::bind("127.0.0.1:0").await.expect("bind");
    let address = listener.local_addr().expect("local addr");
    let server = tokio::spawn(serve_one(listener, "500 Internal Server Error"));

    let publisher = Publisher::new(
        format!("http://{address}/write"),
        Arc::new(|| "metric 1\n".to_string()),
        Arc::new(HttpMetricSender::default()),
    )
    .with_max_attempts(1);
    let error = publisher
        .push_once(&CancellationToken::new())
        .await
        .unwrap_err();
    assert!(error.to_string().contains("500"), "{error}");
    let _ = tokio::time::timeout(Duration::from_secs(2), server).await;
}
