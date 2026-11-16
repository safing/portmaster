//! Port of Go `base/utils/debug`: a buffer that collects debugging
//! information in sections, using the same layout as the Go core.
//!
//! Go's `Info` inherits `bytes.Buffer`; here the buffer is a `String` with
//! explicit accessors. The section layout — including spacers, code fences and
//! the `github` style — is byte-compatible with `debug.Info.AddSection`.

/// No flags.
pub const NO_FLAGS: u8 = 0;
/// Wrap the section content in a markdown code section.
pub const USE_CODE_SECTION: u8 = 1;
/// Add a line break after every content part except the last.
pub const ADD_CONTENT_LINE_BREAKS: u8 = 2;

/// Debug information collector.
#[derive(Debug, Clone, Default)]
pub struct Info {
    buffer: String,
    /// Output style. `github` enables collapsible sections; anything else
    /// uses the default bold markdown header.
    pub style: String,
}

impl Info {
    /// Creates an empty collector.
    pub fn new() -> Info {
        Info::default()
    }

    /// Sets the output style, like assigning `Info.Style` in Go.
    pub fn with_style(mut self, style: &str) -> Info {
        self.style = style.to_string();
        self
    }

    /// Number of bytes written so far, like `bytes.Buffer.Len()`.
    pub fn len(&self) -> usize {
        self.buffer.len()
    }

    /// Whether nothing has been written yet.
    pub fn is_empty(&self) -> bool {
        self.buffer.is_empty()
    }

    /// Appends a section, matching Go `Info.AddSection` byte for byte.
    pub fn add_section(&mut self, name: &str, flags: u8, content: &[&str]) {
        if !self.buffer.is_empty() {
            self.buffer.push_str("\n\n");
        }

        if self.style == "github" {
            self.buffer
                .push_str(&format!("<details>\n<summary>{name}</summary>\n\n"));
        } else {
            self.buffer.push_str(&format!("**{name}**:\n\n"));
        }

        let code = flags & USE_CODE_SECTION != 0;
        let breaks = flags & ADD_CONTENT_LINE_BREAKS != 0;
        if code {
            self.buffer.push_str("```\n");
        }
        for (index, part) in content.iter().enumerate() {
            self.buffer.push_str(part);
            if breaks && index + 1 < content.len() {
                self.buffer.push('\n');
            }
        }
        if code {
            self.buffer.push_str("\n```\n");
        }
        if self.style == "github" {
            self.buffer.push_str("\n</details>");
        }
    }

    /// Adds version information, like Go `Info.AddVersionInfo`.
    ///
    /// The Go package pulls these strings from its own `base/info` package;
    /// callers pass the corresponding values from the Rust build.
    pub fn add_version_info(&mut self, version: &str, full_version: &str) {
        self.add_section(
            &format!("Version {version}"),
            USE_CODE_SECTION,
            &[full_version],
        );
    }

    /// Adds the last unexpected log lines, like Go
    /// `Info.AddLastUnexpectedLogs`. An empty list yields the fixed
    /// "No Unexpected Logs" section.
    pub fn add_last_unexpected_logs(&mut self, lines: &[String], current_time: &str) {
        if lines.is_empty() {
            self.add_section("No Unexpected Logs", NO_FLAGS, &[]);
            return;
        }

        let mut content = lines.iter().map(String::as_str).collect::<Vec<_>>();
        let timestamp = current_time.to_string();
        content.push(&timestamp);
        self.add_section(
            "Unexpected Logs",
            USE_CODE_SECTION | ADD_CONTENT_LINE_BREAKS,
            &content,
        );
    }

    /// Returns the collected text.
    pub fn as_str(&self) -> &str {
        &self.buffer
    }

    /// Consumes the collector and returns the collected text.
    pub fn into_string(self) -> String {
        self.buffer
    }
}

impl std::fmt::Display for Info {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.write_str(&self.buffer)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn matches_go_section_layout() {
        let mut info = Info::new();
        info.add_section("First", NO_FLAGS, &["plain"]);
        info.add_section("Second", USE_CODE_SECTION, &["a", "b"]);
        assert_eq!(
            info.as_str(),
            "**First**:\n\nplain\n\n**Second**:\n\n```\nab\n```\n"
        );
    }

    #[test]
    fn matches_go_line_breaks_and_github_style() {
        let mut info = Info::new().with_style("github");
        info.add_section(
            "Status: Online",
            USE_CODE_SECTION | ADD_CONTENT_LINE_BREAKS,
            &["line 1", "line 2", "line 3"],
        );
        assert_eq!(
            info.as_str(),
            "<details>\n<summary>Status: Online</summary>\n\n```\nline 1\nline 2\nline 3\n```\n\n</details>"
        );
    }

    #[test]
    fn matches_go_version_and_empty_logs() {
        let mut info = Info::new();
        info.add_version_info("v1.2.3", "Portmaster v1.2.3\ncommit abc\n");
        info.add_last_unexpected_logs(&[], "260101 12:00:00.000");
        assert_eq!(
            info.as_str(),
            "**Version v1.2.3**:\n\n```\nPortmaster v1.2.3\ncommit abc\n\n```\n\n\n**No Unexpected Logs**:\n\n"
        );
    }

    #[test]
    fn adds_current_time_to_unexpected_logs() {
        let mut info = Info::new();
        info.add_last_unexpected_logs(&["error one".to_string()], "260101 12:00:00.000");
        assert!(info.as_str().contains("error one\n260101 12:00:00.000\n```"));
    }
}
