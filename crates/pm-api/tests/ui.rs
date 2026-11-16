//! Tests of the UI archive routes.

use std::io::Write;
use std::path::Path;
use std::sync::Arc;

use pm_api::{Api, UiArchives};
use tokio::io::{AsyncReadExt, AsyncWriteExt};

fn create_archive(path: &Path, files: &[(&str, &str)]) {
    let file = std::fs::File::create(path).unwrap();
    let mut writer = zip::ZipWriter::new(file);
    let options = zip::write::SimpleFileOptions::default()
        .compression_method(zip::CompressionMethod::Deflated);
    for (name, data) in files {
        writer.start_file(*name, options).unwrap();
        writer.write_all(data.as_bytes()).unwrap();
    }
    writer.finish().unwrap();
}

fn temp_dir(tag: &str) -> std::path::PathBuf {
    let mut path = std::env::temp_dir();
    path.push(format!(
        "pm-api-ui-{tag}-{}-{}",
        std::process::id(),
        std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)
            .unwrap()
            .subsec_nanos()
    ));
    let _ = std::fs::remove_dir_all(&path);
    std::fs::create_dir_all(&path).unwrap();
    path
}

async fn request(
    address: std::net::SocketAddr,
    method: &str,
    path: &str,
) -> (String, Vec<(String, String)>, Vec<u8>) {
    let mut stream = tokio::net::TcpStream::connect(address).await.unwrap();
    let message = format!(
        "{method} {path} HTTP/1.1\r\nHost: 127.0.0.1\r\nConnection: close\r\nContent-Length: 0\r\n\r\n"
    );
    stream.write_all(message.as_bytes()).await.unwrap();
    let mut raw = Vec::new();
    stream.read_to_end(&mut raw).await.unwrap();
    let text = String::from_utf8_lossy(&raw).into_owned();
    let (head, body) = text.split_once("\r\n\r\n").unwrap_or((&text, ""));
    let mut lines = head.split("\r\n");
    let status = lines.next().unwrap_or_default().to_string();
    let headers: Vec<(String, String)> = lines
        .filter_map(|line| line.split_once(':'))
        .map(|(name, value)| (name.trim().to_lowercase(), value.trim().to_string()))
        .collect();
    (status, headers, body.as_bytes().to_vec())
}

fn header<'a>(headers: &'a [(String, String)], name: &str) -> Option<&'a str> {
    headers
        .iter()
        .find(|(key, _)| key == name)
        .map(|(_, value)| value.as_str())
}

#[tokio::test]
async fn serves_ui_archives() {
    let bin_dir = temp_dir("archives");
    create_archive(
        &bin_dir.join("portmaster.zip"),
        &[
            ("index.html", "<html>portmaster ui</html>"),
            ("main.js", "console.log('ui')"),
            ("styles/app.css", "body{}"),
        ],
    );
    create_archive(
        &bin_dir.join("assets.zip"),
        &[("index.html", "assets index"), ("logo.png", "pngdata")],
    );

    let api = Api::new("127.0.0.1:0");
    api.set_ui_archives(Arc::new(UiArchives::new(&bin_dir)));
    let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await.unwrap();
    let address = listener.local_addr().unwrap();
    let server = tokio::spawn(pm_api::serve(api, listener));

    // Root redirects to the default module.
    let (status, headers, _) = request(address, "GET", "/").await;
    assert!(status.contains("307"), "{status}");
    assert_eq!(
        header(&headers, "location"),
        Some("/ui/modules/portmaster/")
    );

    // Module without a trailing slash redirects.
    let (status, headers, _) = request(address, "GET", "/ui/modules/portmaster").await;
    assert!(status.contains("308"), "{status}");
    assert_eq!(
        header(&headers, "location"),
        Some("/ui/modules/portmaster/")
    );

    // The module index is served.
    let (status, headers, body) = request(address, "GET", "/ui/modules/portmaster/").await;
    assert!(status.contains("200"), "{status}");
    assert_eq!(
        header(&headers, "content-type"),
        Some("text/html; charset=utf-8")
    );
    assert_eq!(body, b"<html>portmaster ui</html>");
    assert_eq!(header(&headers, "x-frame-options"), Some("deny"));

    // Files are served with their content type.
    let (status, headers, body) = request(address, "GET", "/ui/modules/portmaster/main.js").await;
    assert!(status.contains("200"), "{status}");
    assert_eq!(
        header(&headers, "content-type"),
        Some("application/javascript; charset=utf-8")
    );
    assert_eq!(body, b"console.log('ui')");

    let (status, headers, _) =
        request(address, "GET", "/ui/modules/portmaster/styles/app.css").await;
    assert!(status.contains("200"), "{status}");
    assert_eq!(
        header(&headers, "content-type"),
        Some("text/css; charset=utf-8")
    );

    // Unknown client-side routes fall back to the index.
    let (status, _, body) = request(address, "GET", "/ui/modules/portmaster/app/settings").await;
    assert!(status.contains("200"), "{status}");
    assert_eq!(body, b"<html>portmaster ui</html>");

    // Assets are served from the assets archive.
    let (status, headers, body) = request(address, "GET", "/assets/logo.png").await;
    assert!(status.contains("200"), "{status}");
    assert_eq!(header(&headers, "content-type"), Some("image/png"));
    assert_eq!(body, b"pngdata");

    // Unknown modules fail like in Go.
    let (status, _, _) = request(address, "GET", "/ui/modules/missing/").await;
    assert!(status.contains("500"), "{status}");

    // Only read methods are supported.
    let (status, _, _) = request(address, "POST", "/ui/modules/portmaster/").await;
    assert!(status.contains("405"), "{status}");

    server.abort();
}

#[tokio::test]
async fn without_archives_the_ui_reports_not_configured() {
    let api = Api::new("127.0.0.1:0");
    let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await.unwrap();
    let address = listener.local_addr().unwrap();
    let server = tokio::spawn(pm_api::serve(api, listener));

    let (status, _, body) = request(address, "GET", "/ui/modules/portmaster/").await;
    assert!(status.contains("404"), "{status}");
    assert_eq!(body, b"ui is not configured\n");

    server.abort();
}

#[tokio::test]
async fn upgrade_lock_blocks_resources_and_reload_clears_archives() {
    let bin_dir = temp_dir("upgrade-lock");
    create_archive(
        &bin_dir.join("portmaster.zip"),
        &[("index.html", "<html>ui</html>")],
    );

    let api = Api::new("127.0.0.1:0");
    api.set_dev_mode(true);
    let archives = Arc::new(UiArchives::new(&bin_dir));
    api.set_ui_archives(Arc::clone(&archives));
    pm_api::ui::register_ui_endpoints(&api).expect("register ui endpoints");

    let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await.unwrap();
    let address = listener.local_addr().unwrap();
    let server = tokio::spawn(pm_api::serve(api, listener));

    let (status, _, _) = request(address, "GET", "/ui/modules/portmaster/").await;
    assert!(status.contains("200"), "{status}");

    // Enable the lock: resources return 423 like in Go.
    archives.enable_upgrade_lock();
    assert!(archives.is_upgrade_locked());
    let (status, _, body) = request(address, "GET", "/ui/modules/portmaster/").await;
    assert!(status.contains("423"), "{status}");
    assert_eq!(body, b"Resources locked, upgrade in progress.\n");

    archives.disable_upgrade_lock();
    let (status, _, _) = request(address, "GET", "/ui/modules/portmaster/").await;
    assert!(status.contains("200"), "{status}");

    // The UI reload action drops the cached archive without an API lock.
    let (status, _, body) = request(address, "POST", "/api/v1/ui/reload").await;
    assert!(status.contains("200"), "{status}");
    assert_eq!(body, b"all ui archives successfully reloaded\n");

    server.abort();
}
