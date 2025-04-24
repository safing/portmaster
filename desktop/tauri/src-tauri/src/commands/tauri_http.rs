use tauri::State;
use reqwest::{Client, Method}; 
use serde::{Deserialize, Serialize};

/// Creates and configures a shared HTTP client for application-wide use.
/// 
/// Returns a reqwest Client configured with:
/// - Connection pooling
/// - Persistent cookie store
/// 
/// Client can be accessed from UI through the exposed Tauri command `send_tauri_http_request(...)`
/// Such requests execute directly from the Tauri app binary, not from the WebView process
pub fn create_http_client() -> Client {
    Client::builder()
        // Maximum idle connections per host
        .pool_max_idle_per_host(10)
        // Enable cookie support
        .cookie_store(true)
        .user_agent("Portmaster UI")
        .build()
        .expect("failed to build HTTP client")
}

#[derive(Deserialize)]
pub struct HttpRequestOptions {
  method: String,
  headers: Vec<(String, String)>,
  body: Option<Vec<u8>>,
}

#[derive(Serialize)]
pub struct HttpResponse {
  status: u16,
  status_text: String,
  headers: Vec<(String, String)>,
  body: Vec<u8>,
}

#[tauri::command]
pub async fn send_tauri_http_request(
  client: State<'_, Client>,
  url: String,
  opts: HttpRequestOptions
) -> Result<HttpResponse, String> {
  //println!("URL: {}", url);

  // Build the request
  let mut req = client
    .request(Method::from_bytes(opts.method.as_bytes()).map_err(|e| e.to_string())?, &url);

  // Apply headers
  for (k, v) in opts.headers {
    req = req.header(&k, &v);
  }

  // Attach body if present
  if let Some(body) = opts.body {
    req = req.body(body);
  }

  // Send and await the response
  let resp = req.send().await.map_err(|e| e.to_string())?;

  // Read status, headers, and body
  let status = resp.status().as_u16();
  let status_text = resp.status().canonical_reason().unwrap_or("").to_string();
  let headers = resp
    .headers()
    .iter()
    .map(|(k, v)| (k.to_string(), v.to_str().unwrap_or("").to_string()))
    .collect();
  let body = resp.bytes().await.map_err(|e| e.to_string())?.to_vec();

  Ok(HttpResponse { status, status_text, headers, body })
}
