//! Binary metadata: human readable names and profile icons.
//!
//! Port of Go `service/profile/binmeta`. The name generation, file-description
//! cleanup, icon ignore list and icon storage rules are pure and tested on
//! Linux. Extracting icons from PE resources is Windows-gated; see
//! [`extract_from_exe`] for the current limitation.

use std::path::Path;

use sha1::{Digest, Sha1};

/// Icon types, matching Go `binmeta.IconType`.
pub const ICON_TYPE_API: &str = "api";
/// Icon sources, matching Go `binmeta.IconSource`.
pub const ICON_SOURCE_CORE: &str = "core";

/// Errors of the binary metadata helpers.
#[derive(Debug, thiserror::Error)]
pub enum BinMetaError {
    /// The icon data exceeds the size limit.
    #[error("icon too big")]
    IconTooBig,
    /// The icon hash is on the ignore list.
    #[error("icon is ignored")]
    IconIgnored,
    /// The icon format is not supported.
    #[error("unsupported icon format")]
    UnsupportedIconFormat,
    /// A file could not be read.
    #[error("failed to read {0}: {1}")]
    Read(String, String),
    /// A file could not be written.
    #[error("failed to write {0}: {1}")]
    Write(String, String),
}

/// Result type of the binary metadata helpers.
pub type Result<T> = std::result::Result<T, BinMetaError>;

const MAX_ICON_SIZE: usize = 1_000_000;

/// Icon hashes that should be ignored (Go `ignoreIcons`).
const IGNORE_ICONS: &[&str] = &[
    "a27898ddfa4e0481b62c69faa196919a738fcade",
    "5a3eea8bcd08b9336ce9c5083f26185164268ee9",
    "573393d6ad238d255b20dc1c1b303c95debe6965",
    "d459b2cb23c27cc31ccab5025533048d5d8301bf",
    "d35a0d91ebfda81df5286f68ec5ddb1d6ad6b850",
    "cc33187385498384f1b648e23be5ef1a2e9f5f71",
];

/// Extensions whose final path segment is stripped by
/// [`generate_binary_name_from_path`].
const BINARY_EXTENSIONS: &[&str] = &[
    ".exe", ".msi", ".bat", ".cmd", ".ps1", ".run", ".appimage", ".app", ".action", ".out",
];

fn is_ascii_alnum(c: char) -> bool {
    c.is_ascii_alphanumeric()
}

/// Splits a file name into the segments produced by Go's
/// `segmentsSplitter` regex (`[^A-Za-z0-9]*[A-Z]?[a-z0-9]*`).
fn split_segments(file_name: &str) -> Vec<String> {
    let chars: Vec<char> = file_name.chars().collect();
    let mut segments = Vec::new();
    let mut index = 0;

    while index < chars.len() {
        let mut segment = String::new();
        while index < chars.len() && !is_ascii_alnum(chars[index]) {
            segment.push(chars[index]);
            index += 1;
        }
        if index < chars.len() && chars[index].is_ascii_uppercase() {
            segment.push(chars[index]);
            index += 1;
        }
        while index < chars.len()
            && (chars[index].is_ascii_lowercase() || chars[index].is_ascii_digit())
        {
            segment.push(chars[index]);
            index += 1;
        }
        // The pattern can only be empty at end of input, which the loop above
        // already excludes; this guard keeps the loop moving for odd inputs.
        if segment.is_empty() {
            segment.push(chars[index]);
            index += 1;
        }
        segments.push(segment);
    }

    segments
}

fn strip_leading_delimiters(input: &str) -> &str {
    input.trim_start_matches(|c: char| !is_ascii_alnum(c))
}

fn is_name_only(input: &str) -> bool {
    !input.is_empty() && input.chars().all(is_ascii_alnum)
}

fn is_delimiters_only(input: &str) -> bool {
    !input.is_empty() && input.chars().all(|c| !is_ascii_alnum(c))
}

/// Upper-cases the first character, like Go's `strings.Title` for a
/// single-word, alphanumeric segment.
fn title_case(input: &str) -> String {
    let mut chars = input.chars();
    match chars.next() {
        Some(first) => first.to_uppercase().collect::<String>() + chars.as_str(),
        None => String::new(),
    }
}

/// Generates a human readable binary name from a path, like Go
/// `GenerateBinaryNameFromPath`.
pub fn generate_binary_name_from_path(path: &str) -> String {
    // filepath.Split: keep everything after the last separator (Windows Go
    // treats both slashes as separators).
    let file_name = path
        .rsplit(['/', '\\'])
        .next()
        .unwrap_or(path);

    let mut segments = split_segments(file_name);
    if segments.len() >= 2 {
        let last = segments.last().expect("checked length").to_ascii_lowercase();
        if BINARY_EXTENSIONS.contains(&last.as_str()) {
            segments.pop();
        }
    }

    let mut name_parts: Vec<String> = Vec::with_capacity(segments.len());
    let mut fragments = String::new();
    for segment in segments {
        if strip_leading_delimiters(&segment).len() <= 2 {
            fragments.push_str(&segment);
            continue;
        }
        if !fragments.is_empty() {
            name_parts.push(std::mem::take(&mut fragments));
        }
        name_parts.push(segment);
    }
    if !fragments.is_empty() {
        name_parts.push(fragments);
    }

    for part in &mut name_parts {
        *part = strip_leading_delimiters(part).to_string();
        if is_name_only(part) {
            *part = title_case(part);
        }
    }

    name_parts.join(" ")
}

/// Cleans a version resource's file description, like Go
/// `cleanFileDescription`.
pub fn clean_file_description(description: &str) -> String {
    let mut fields: Vec<String> = description
        .split_whitespace()
        .map(|field| field.replace(['"', '\''], ""))
        .collect();

    let mut end_index = fields.len();
    for (index, field) in fields.iter().enumerate() {
        if index >= 1 && field.len() <= 2 && !is_name_only(field) {
            end_index = index;
            break;
        }
    }
    fields.truncate(end_index);

    let mut name = fields.join(" ");
    if let Some((first, _)) = name.split_once(". ") {
        name = first.to_string();
    }
    if is_delimiters_only(&name) {
        return String::new();
    }
    name.trim().to_string()
}

/// Returns whether an icon hash should be ignored, like Go `IgnoreIcon`.
pub fn ignore_icon(name: &str) -> bool {
    let lower = name.to_ascii_lowercase();
    let hash = lower.split_once('.').map_or(lower.as_str(), |(hash, _)| hash);
    IGNORE_ICONS.contains(&hash)
}

/// Computes the storage filename for an icon, like Go `UpdateProfileIcon`.
pub fn icon_filename(data: &[u8], extension: &str) -> Result<String> {
    if data.len() > MAX_ICON_SIZE {
        return Err(BinMetaError::IconTooBig);
    }

    let sum = hex_encode(&Sha1::digest(data));
    if ignore_icon(&sum) {
        return Err(BinMetaError::IconIgnored);
    }

    let mut ext = extension.to_ascii_lowercase();
    match ext.as_str() {
        "jpeg" => ext = "jpg".to_string(),
        "gif" | "jpg" | "png" | "svg" | "tiff" | "webp" => {}
        _ => return Err(BinMetaError::UnsupportedIconFormat),
    }

    Ok(format!("{sum}.{ext}"))
}

fn hex_encode(bytes: &[u8]) -> String {
    const HEX: &[u8; 16] = b"0123456789abcdef";
    let mut result = String::with_capacity(bytes.len() * 2);
    for byte in bytes {
        result.push(HEX[(byte >> 4) as usize] as char);
        result.push(HEX[(byte & 0x0F) as usize] as char);
    }
    result
}

/// An icon resolved from a binary or an icon file.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct IconResult {
    pub icon_type: String,
    pub icon_value: String,
    pub icon_source: String,
}

impl IconResult {
    fn api(value: String) -> Self {
        Self {
            icon_type: ICON_TYPE_API.to_string(),
            icon_value: value,
            icon_source: ICON_SOURCE_CORE.to_string(),
        }
    }
}

/// The name and icon of a binary.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct BinaryMeta {
    pub icon: Option<IconResult>,
    pub name: String,
}

/// Stores icon bytes in `storage_path` and returns an API icon reference, like
/// Go `UpdateProfileIcon` + the `Icon` construction in `GetIconAndName`.
pub fn load_and_save_icon(icon_path: &Path, storage_path: &Path) -> Result<IconResult> {
    let data = std::fs::read(icon_path)
        .map_err(|err| BinMetaError::Read(icon_path.display().to_string(), err.to_string()))?;
    let ext = icon_path
        .extension()
        .and_then(|ext| ext.to_str())
        .unwrap_or("");
    let filename = icon_filename(&data, ext)?;
    let target = storage_path.join(&filename);
    std::fs::write(&target, &data)
        .map_err(|err| BinMetaError::Write(target.display().to_string(), err.to_string()))?;
    Ok(IconResult::api(filename))
}

/// Extracts a product name and PNG icon from a PE executable.
///
/// The Go implementation uses `winres` to read the version resource and the
/// first group icon, then converts the ICO to PNG. That PE resource parser is
/// **not ported yet**; this function only reports whether the file exists.
/// Callers still get a valid fallback name from
/// [`generate_binary_name_from_path`], which is the important half for profile
/// matching. See the porting report for details.
#[cfg(windows)]
pub fn extract_from_exe(bin_path: &Path) -> Result<(Option<Vec<u8>>, String)> {
    match std::fs::metadata(bin_path) {
        Ok(_) => {
            tracing::debug!(
                path = %bin_path.display(),
                "binmeta: PE resource extraction is not available; using path-derived name"
            );
            Ok((None, String::new()))
        }
        Err(err) if err.kind() == std::io::ErrorKind::NotFound => Ok((None, String::new())),
        Err(err) => Err(BinMetaError::Read(bin_path.display().to_string(), err.to_string())),
    }
}

/// Non-Windows stub for [`extract_from_exe`].
#[cfg(not(windows))]
pub fn extract_from_exe(_bin_path: &Path) -> Result<(Option<Vec<u8>>, String)> {
    Ok((None, String::new()))
}

/// Extracts the app directory name from a Windows Store executable path.
///
/// Mirrors the prefix scan in Go's `WinStoreHandler.AddTags`. `store_paths`
/// are directories such as `C:\Program Files\WindowsApps\`.
pub fn winstore_app_dir(path: &str, store_paths: &[String]) -> Option<String> {
    for store_path in store_paths {
        if let Some(rest) = path.strip_prefix(store_path) {
            let app_dir = rest.split('\\').next().unwrap_or(rest);
            if !app_dir.is_empty() {
                return Some(app_dir.to_string());
            }
        }
    }
    None
}

/// Parses a Windows Store app directory name into `(app name, publisher id)`.
///
/// Example: `Microsoft.Office.OneNote_17.6769.57631.0_x64__8wekyb3d8bbwe`.
/// Returns `None` when the directory does not have Go's expected five fields.
pub fn parse_winstore_app_dir(app_dir: &str) -> Option<(String, String)> {
    let parts: Vec<&str> = app_dir.split('_').collect();
    if parts.len() != 5 {
        return None;
    }
    Some((parts[0].to_string(), parts[4].to_string()))
}

/// Returns the name and icon of a binary, like Go `GetIconAndName`.
pub fn get_icon_and_name(
    bin_path: &Path,
    _home_dir: &str,
    storage_path: &Path,
) -> Result<BinaryMeta> {
    let (png, mut name) = extract_from_exe(bin_path)?;
    if name.is_empty() {
        name = generate_binary_name_from_path(&bin_path.to_string_lossy());
    }

    let icon = match png {
        Some(png) => Some(IconResult::api(icon_filename(&png, "png")?)),
        None => None,
    };
    // `storage_path` is kept in the signature to mirror Go's global icon
    // storage; the current extractor produces no PNG to store yet.
    let _ = storage_path;

    Ok(BinaryMeta { icon, name })
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn generates_names_like_go() {
        let cases = [
            ("nslookup.exe", "Nslookup"),
            ("SystemSettings.exe", "System Settings"),
            ("OneDriveSetup.exe", "One Drive Setup"),
            ("msedge.exe", "Msedge"),
            ("SIHClient.exe", "SIH Client"),
            ("openvpn-gui.exe", "Openvpn Gui"),
            ("portmaster-core_v0-1-2.exe", "Portmaster Core v0-1-2"),
            ("WinStore.App.exe", "Win Store App"),
            (".test-script", "Test Script"),
            ("browser_broker.exe", "Browser Broker"),
            ("VirtualBoxVM", "Virtual Box VM"),
            ("io.elementary.appcenter", "Io Elementary Appcenter"),
            ("Microsoft.WindowsStore", "Microsoft Windows Store"),
        ];
        for (input, expected) in cases {
            assert_eq!(generate_binary_name_from_path(input), expected, "input: {input}");
        }
    }

    #[test]
    fn cleans_file_descriptions_like_go() {
        let cases = [
            ("Product Name", "Product Name"),
            ("Product Name. Does this and that.", "Product Name"),
            ("Product Name - Does this and that.", "Product Name"),
            ("Product Name / Does this and that.", "Product Name"),
            ("Product Name :: Does this and that.", "Product Name"),
            ("/ Product Name", "/ Product Name"),
            ("Product / Name", "Product"),
            ("Software 2", "Software 2"),
            ("Launcher for 'Software 2'", "Launcher for Software 2"),
            (". / Name", ""),
            (". ", ""),
            (".", ""),
            ("N/A", "N/A"),
            ("Product Name a Does this and that.", "Product Name a Does this and that."),
        ];
        for (input, expected) in cases {
            assert_eq!(clean_file_description(input), expected, "input: {input}");
        }
    }

    #[test]
    fn ignores_known_icons() {
        assert!(ignore_icon("a27898ddfa4e0481b62c69faa196919a738fcade.png"));
        assert!(ignore_icon("A27898DDFA4E0481B62C69FAA196919A738FCADE"));
        assert!(!ignore_icon("deadbeef.png"));
    }

    #[test]
    fn computes_icon_filenames() {
        // SHA-1 of "hello" is aaf4c61ddcc5e8a2dabede0f3b482cd9aea9434d.
        assert_eq!(
            icon_filename(b"hello", "PNG").unwrap(),
            "aaf4c61ddcc5e8a2dabede0f3b482cd9aea9434d.png"
        );
        assert_eq!(
            icon_filename(b"hello", "jpeg").unwrap(),
            "aaf4c61ddcc5e8a2dabede0f3b482cd9aea9434d.jpg"
        );
        assert!(icon_filename(b"hello", "bmp").is_err());
        assert!(icon_filename(&vec![0u8; MAX_ICON_SIZE + 1], "png").is_err());
    }

    #[test]
    fn parses_winstore_paths() {
        let paths = vec![r"C:\Program Files\WindowsApps\".to_string()];
        let path = r"C:\Program Files\WindowsApps\Microsoft.Office.OneNote_17.6769.57631.0_x64__8wekyb3d8bbwe\ONENOTE.EXE";
        let app_dir = winstore_app_dir(path, &paths).unwrap();
        assert_eq!(app_dir, "Microsoft.Office.OneNote_17.6769.57631.0_x64__8wekyb3d8bbwe");
        assert_eq!(
            parse_winstore_app_dir(&app_dir),
            Some(("Microsoft.Office.OneNote".to_string(), "8wekyb3d8bbwe".to_string()))
        );
        assert_eq!(parse_winstore_app_dir("Bad_Format"), None);
        assert_eq!(winstore_app_dir(r"C:\Tools\app.exe", &paths), None);
    }

    #[test]
    fn falls_back_to_path_derived_name() {
        let meta = get_icon_and_name(Path::new("C:\\Tools\\OneDriveSetup.exe"), "", Path::new("/tmp")).unwrap();
        assert_eq!(meta.name, "One Drive Setup");
        assert!(meta.icon.is_none());
    }
}
