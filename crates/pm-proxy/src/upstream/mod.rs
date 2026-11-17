pub mod http_connect;
pub mod socks4;
pub mod socks5;

use std::io;
use std::net::SocketAddr;
use std::time::Duration;
use tokio::net::TcpStream;
use url::Url;

use crate::binding::LocalBinding;
use crate::dial::{connect_tcp, resolve_peers};

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

    /// Resolves the proxy address and connects, without a local binding.
    pub async fn dial_and_handshake(
        &self,
        target_host: &str,
        target_port: u16,
        timeout: Duration,
    ) -> io::Result<TcpStream> {
        let peers = resolve_peers(&self.host, self.port, timeout).await?;
        let mut last_error = None;
        for proxy_addr in peers {
            match self
                .dial_and_handshake_to(proxy_addr, target_host, target_port, None, timeout)
                .await
            {
                Ok(stream) => return Ok(stream),
                Err(error) => last_error = Some(error),
            }
        }
        Err(last_error.unwrap_or_else(|| {
            io::Error::new(io::ErrorKind::NotFound, "proxy host has no addresses")
        }))
    }

    /// Connects to an already-resolved proxy address and performs the
    /// handshake. `binding` applies the selected local IP/interface to the
    /// socket that connects to the proxy server.
    pub async fn dial_and_handshake_to(
        &self,
        proxy_addr: SocketAddr,
        target_host: &str,
        target_port: u16,
        binding: Option<&LocalBinding>,
        timeout: Duration,
    ) -> io::Result<TcpStream> {
        let mut stream = connect_tcp(proxy_addr, binding, timeout).await?;
        let host_to_send = self.host_to_send(target_host);

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

    /// Returns the destination host to send in the proxy handshake.
    ///
    /// The domain override taken from the connection is only used for schemes
    /// that actually send a hostname, and only when it is a valid hostname.
    /// All other schemes receive the destination IP. This mirrors Go
    /// `UpstreamProxy.dial` and keeps untrusted domains out of the HTTP CONNECT
    /// request line (CRLF injection).
    fn host_to_send<'a>(&'a self, target_host: &'a str) -> &'a str {
        if self.scheme.sends_hostname() {
            if let Some(override_host) = &self.target_host_override {
                let candidate = override_host.as_str();
                let trimmed = candidate.strip_suffix('.').unwrap_or(candidate);
                if is_valid_hostname(trimmed) {
                    return trimmed;
                }
            }
        }
        target_host
    }
}

/// Reports whether `s` is a plausible DNS hostname that can be safely embedded
/// in any proxy protocol request, like Go `isValidHostname`.
fn is_valid_hostname(s: &str) -> bool {
    if s.is_empty() || s.len() > 253 {
        return false;
    }
    s.bytes()
        .all(|c| c.is_ascii_alphanumeric() || c == b'.' || c == b'-' || c == b'_')
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn hostname_validation_matches_go() {
        assert!(is_valid_hostname("example.com"));
        assert!(is_valid_hostname("a-b_c.example.org"));
        assert!(!is_valid_hostname(""));
        assert!(!is_valid_hostname("bad host"));
        assert!(!is_valid_hostname("evil\r\nHost: attacker"));
        assert!(!is_valid_hostname(&"a".repeat(254)));
    }

    #[test]
    fn override_is_honored_only_for_hostname_schemes() {
        let mut proxy = UpstreamProxy::parse("socks5://127.0.0.1:1080").unwrap();
        proxy.target_host_override = Some("example.com".to_string());
        assert_eq!(proxy.host_to_send("1.1.1.1"), "1.1.1.1");

        proxy.scheme = UpstreamScheme::Socks5h;
        assert_eq!(proxy.host_to_send("1.1.1.1"), "example.com");

        proxy.target_host_override = Some("bad host\r\n".to_string());
        assert_eq!(proxy.host_to_send("1.1.1.1"), "1.1.1.1");
    }
}
