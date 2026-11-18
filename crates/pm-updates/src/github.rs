//! GitHub Release update checker for Portmaster-Rust.

use serde::Deserialize;
use crate::downloader::HttpClient;
use crate::version::Version;

/// Metadata of a release returned by GitHub Releases API.
#[derive(Debug, Clone, Deserialize)]
pub struct GithubRelease {
    pub tag_name: String,
    pub name: Option<String>,
    pub html_url: String,
    pub body: Option<String>,
    pub published_at: Option<String>,
}

/// Result of checking GitHub for releases.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum GithubCheckResult {
    UpdateAvailable {
        current: String,
        latest: String,
        release_name: String,
        html_url: String,
    },
    UpToDate {
        current: String,
    },
    Error(String),
}

/// Checks the specified GitHub repository (`owner/repo`) for the latest release.
pub fn check_github_repo_release(
    client: &dyn HttpClient,
    repo: &str,
    current_version: &str,
) -> GithubCheckResult {
    let repo = repo.trim().trim_matches('/');
    if repo.is_empty() {
        return GithubCheckResult::Error("empty GitHub repository specified".to_string());
    }

    let url = format!("https://api.github.com/repos/{repo}/releases/latest");
    let bytes = match client.get(&url) {
        Ok(bytes) => bytes,
        Err(err) => {
            return GithubCheckResult::Error(format!("failed to fetch GitHub release: {err}"));
        }
    };

    let release: GithubRelease = match serde_json::from_slice(&bytes) {
        Ok(rel) => rel,
        Err(err) => {
            return GithubCheckResult::Error(format!("failed to parse GitHub release JSON: {err}"));
        }
    };

    let latest_tag = release.tag_name.trim();
    let current = current_version.trim();

    let cur_v = Version::parse(current);
    let lat_v = Version::parse(latest_tag);

    let is_newer = match (&cur_v, &lat_v) {
        (Ok(c), Ok(l)) => l > c,
        _ => latest_tag != current && !latest_tag.is_empty(),
    };

    if is_newer {
        GithubCheckResult::UpdateAvailable {
            current: current.to_string(),
            latest: latest_tag.to_string(),
            release_name: release.name.unwrap_or_else(|| latest_tag.to_string()),
            html_url: release.html_url,
        }
    } else {
        GithubCheckResult::UpToDate {
            current: current.to_string(),
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    struct MockClient(Vec<u8>);
    impl HttpClient for MockClient {
        fn get(&self, _url: &str) -> std::result::Result<Vec<u8>, String> {
            Ok(self.0.clone())
        }
    }

    #[test]
    fn detects_newer_release() {
        let json = br#"{
            "tag_name": "v2.2.11",
            "name": "Portmaster v2.2.11",
            "html_url": "https://github.com/VLOD-ZDOV/portmaster-rust/releases/tag/v2.2.11"
        }"#;
        let client = MockClient(json.to_vec());
        let res = check_github_repo_release(&client, "VLOD-ZDOV/portmaster-rust", "2.2.10");
        assert_eq!(
            res,
            GithubCheckResult::UpdateAvailable {
                current: "2.2.10".into(),
                latest: "v2.2.11".into(),
                release_name: "Portmaster v2.2.11".into(),
                html_url: "https://github.com/VLOD-ZDOV/portmaster-rust/releases/tag/v2.2.11".into(),
            }
        );
    }

    #[test]
    fn detects_up_to_date_release() {
        let json = br#"{
            "tag_name": "v2.2.10",
            "name": "Portmaster v2.2.10",
            "html_url": "https://github.com/VLOD-ZDOV/portmaster-rust/releases/tag/v2.2.10"
        }"#;
        let client = MockClient(json.to_vec());
        let res = check_github_repo_release(&client, "VLOD-ZDOV/portmaster-rust", "2.2.10");
        assert_eq!(res, GithubCheckResult::UpToDate { current: "2.2.10".into() });
    }
}
