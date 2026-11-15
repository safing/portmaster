pub mod http_connect;
pub mod socks4;
pub mod socks5;

use std::io;
use std::time::Duration;
use tokio::net::TcpStream;
use url::Url;

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum UpstreamScheme {
    Http,
    Socks4,
    Socks4a,
    Socks5,
    Socks5h,
}

impl UpstreamScheme {
    pub fn parse(s: &str) -> Option<Self> {
        match s {
            "http" => Some(Self::Http),
            "socks4" => Some(Self::Socks4),
            "socks4a" => Some(Self::Socks4a),
            "socks5" => Some(Self::Socks5),
            "socks5h" => Some(Self::Socks5h),
            _ => None,
        }
    }

    pub fn sends_hostname(self) -> bool {
        matches!(self, Self::Http | Self::Socks4a | Self::Socks5h)
    }

    pub fn supported_schemes() -> &'static [&'static str] {
        &["http", "socks4", "socks4a", "socks5", "socks5h"]
    }
}

/// UpstreamProxy represents an outbound proxy configuration.
#[derive(Debug, Clone)]
pub struct UpstreamProxy {
    pub scheme: UpstreamScheme,
    pub host: String,
    pub port: u16,
    pub username: Option<String>,
    pub password: Option<String>,
    pub target_host_override: Option<String>,
}

impl UpstreamProxy {
    pub fn parse(raw_url: &str) -> Result<Self, String> {
        let parsed = Url::parse(raw_url).map_err(|e| format!("proxy URL is malformed: {}", e))?;
        let scheme = UpstreamScheme::parse(parsed.scheme()).ok_or_else(|| {
            format!(
                "unsupported proxy scheme '{}', supported are: {:?}",
                parsed.scheme(),
                UpstreamScheme::supported_schemes()
            )
        })?;

        let host = parsed.host_str().ok_or("proxy URL is missing the host")?.to_string();
        let port = parsed.port().ok_or("proxy URL is missing a valid port")?;

        let username = if parsed.username().is_empty() {
            None
        } else {
            Some(parsed.username().to_string())
        };
        let password = parsed.password().map(|p| p.to_string());

        Ok(Self {
            scheme,
            host,
            port,
            username,
            password,
            target_host_override: None,
        })
    }

    pub async fn dial_and_handshake(
        &self,
        target_host: &str,
        target_port: u16,
        timeout: Duration,
    ) -> io::Result<TcpStream> {
        let proxy_addr = format!("{}:{}", self.host, self.port);
        let dial_fut = TcpStream::connect(&proxy_addr);
        let mut stream = tokio::time::timeout(timeout, dial_fut)
            .await
            .map_err(|_| io::Error::new(io::ErrorKind::TimedOut, "proxy dial timeout"))??;

        let host_to_send = self.target_host_override.as_deref().unwrap_or(target_host);

        let handshake_fut = async {
            match self.scheme {
                UpstreamScheme::Http => {
                    http_connect::http_connect(
                        &mut stream,
                        self.username.as_deref(),
                        self.password.as_deref(),
                        host_to_send,
                        target_port,
                    ).await
                }
                UpstreamScheme::Socks4 => {
                    socks4::socks4_connect(
                        &mut stream,
                        self.username.as_deref(),
                        host_to_send,
                        target_port,
                        false,
                    ).await
                }
                UpstreamScheme::Socks4a => {
                    socks4::socks4_connect(
                        &mut stream,
                        self.username.as_deref(),
                        host_to_send,
                        target_port,
                        true,
                    ).await
                }
                UpstreamScheme::Socks5 | UpstreamScheme::Socks5h => {
                    socks5::socks5_connect(
                        &mut stream,
                        self.username.as_deref(),
                        self.password.as_deref(),
                        host_to_send,
                        target_port,
                    ).await
                }
            }
        };

        tokio::time::timeout(timeout, handshake_fut)
            .await
            .map_err(|_| io::Error::new(io::ErrorKind::TimedOut, "proxy handshake timeout"))??;

        Ok(stream)
    }
}
