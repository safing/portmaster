//! MIME type lookup ported from Go `base/utils/mimetypes.go`.
//!
//! Portmaster deliberately does not ask the OS for MIME types: a Windows
//! update once broke automatic extension typing in April 2021. The table and
//! the default value are copied verbatim.

/// Default MIME type returned for unknown extensions.
pub const DEFAULT_MIME_TYPE: &str = "application/octet-stream";

/// Returns the MIME type for a file name extension (including the leading dot).
///
/// The extension lookup is case-insensitive. Unknown extensions return
/// ([`DEFAULT_MIME_TYPE`], `false`), exactly like Go's `MimeTypeByExtension`.
pub fn mime_type_by_extension(ext: &str) -> (&'static str, bool) {
    let lower = ext.to_lowercase();
    let mime = match lower.as_str() {
        ".7z" => "application/x-7z-compressed",
        ".atom" => "application/atom+xml",
        ".css" => "text/css; charset=utf-8",
        ".csv" => "text/csv; charset=utf-8",
        ".deb" => "application/x-debian-package",
        ".epub" => "application/epub+zip",
        ".es" => "application/ecmascript",
        ".flv" => "video/x-flv",
        ".gif" => "image/gif",
        ".gz" => "application/gzip",
        ".htm" => "text/html; charset=utf-8",
        ".html" => "text/html; charset=utf-8",
        ".jpeg" => "image/jpeg",
        ".jpg" => "image/jpeg",
        ".js" => "text/javascript; charset=utf-8",
        ".json" => "application/json; charset=utf-8",
        ".m3u" => "audio/mpegurl",
        ".m4a" => "audio/mpeg",
        ".md" => "text/markdown; charset=utf-8",
        ".mjs" => "text/javascript; charset=utf-8",
        ".mov" => "video/quicktime",
        ".mp3" => "audio/mpeg",
        ".mp4" => "video/mp4",
        ".mpeg" => "video/mpeg",
        ".mpg" => "video/mpeg",
        ".ogg" => "audio/ogg",
        ".ogv" => "video/ogg",
        ".otf" => "font/otf",
        ".pdf" => "application/pdf",
        ".png" => "image/png",
        ".qt" => "video/quicktime",
        ".rar" => "application/rar",
        ".rtf" => "application/rtf",
        ".svg" => "image/svg+xml",
        ".tar" => "application/x-tar",
        ".tiff" => "image/tiff",
        ".ts" => "video/MP2T",
        ".ttc" => "font/collection",
        ".ttf" => "font/ttf",
        ".txt" => "text/plain; charset=utf-8",
        ".wasm" => "application/wasm",
        ".wav" => "audio/x-wav",
        ".webm" => "video/webm",
        ".webp" => "image/webp",
        ".woff" => "font/woff",
        ".woff2" => "font/woff2",
        ".xml" => "text/xml; charset=utf-8",
        ".xz" => "application/x-xz",
        ".yaml" => "application/yaml; charset=utf-8",
        ".yml" => "application/yaml; charset=utf-8",
        ".zip" => "application/zip",
        _ => return (DEFAULT_MIME_TYPE, false),
    };
    (mime, true)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn known_extensions_match_the_go_table() {
        let cases = [
            (".html", "text/html; charset=utf-8"),
            (".htm", "text/html; charset=utf-8"),
            (".json", "application/json; charset=utf-8"),
            (".js", "text/javascript; charset=utf-8"),
            (".mjs", "text/javascript; charset=utf-8"),
            (".css", "text/css; charset=utf-8"),
            (".txt", "text/plain; charset=utf-8"),
            (".png", "image/png"),
            (".jpg", "image/jpeg"),
            (".jpeg", "image/jpeg"),
            (".gif", "image/gif"),
            (".svg", "image/svg+xml"),
            (".webp", "image/webp"),
            (".zip", "application/zip"),
            (".gz", "application/gzip"),
            (".7z", "application/x-7z-compressed"),
            (".pdf", "application/pdf"),
            (".mp3", "audio/mpeg"),
            (".mp4", "video/mp4"),
            (".webm", "video/webm"),
            (".ts", "video/MP2T"),
            (".ttf", "font/ttf"),
            (".woff2", "font/woff2"),
            (".yaml", "application/yaml; charset=utf-8"),
            (".yml", "application/yaml; charset=utf-8"),
        ];
        for (ext, expected) in cases {
            assert_eq!(mime_type_by_extension(ext), (expected, true), "{ext}");
        }
    }

    #[test]
    fn lookup_is_case_insensitive() {
        assert_eq!(
            mime_type_by_extension(".HTML"),
            ("text/html; charset=utf-8", true)
        );
        assert_eq!(
            mime_type_by_extension(".JSON"),
            ("application/json; charset=utf-8", true)
        );
        assert_eq!(mime_type_by_extension(".WASM"), ("application/wasm", true));
    }

    #[test]
    fn unknown_extensions_return_default_and_false() {
        for ext in ["", ".", ".unknown", "html", "tar.gz", " application/json"] {
            assert_eq!(
                mime_type_by_extension(ext),
                (DEFAULT_MIME_TYPE, false),
                "{ext:?}"
            );
        }
    }
}
