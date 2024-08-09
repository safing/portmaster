package utils

import "strings"

// Do not depend on the OS for mimetypes.
// A Windows update screwed us over here and broke all the automatic mime
// typing via Go in April 2021.

// MimeTypeByExtension returns a mimetype for the given file name extension,
// which must including the leading dot.
// If the extension is not known, the call returns with ok=false and,
// additionally, a default "application/octet-stream" mime type is returned.
func MimeTypeByExtension(ext string) (mimeType string, ok bool) {
	mimeType, ok = mimeTypes[strings.ToLower(ext)]
	if ok {
		return
	}

	return defaultMimeType, false
}

var (
	defaultMimeType = "application/octet-stream"

	mimeTypes = map[string]string{
		".7z":    "application/x-7z-compressed",
		".atom":  "application/atom+xml",
		".css":   "text/css; charset=utf-8",
		".csv":   "text/csv; charset=utf-8",
		".deb":   "application/x-debian-package",
		".epub":  "application/epub+zip",
		".es":    "application/ecmascript",
		".flv":   "video/x-flv",
		".gif":   "image/gif",
		".gz":    "application/gzip",
		".htm":   "text/html; charset=utf-8",
		".html":  "text/html; charset=utf-8",
		".jpeg":  "image/jpeg",
		".jpg":   "image/jpeg",
		".js":    "text/javascript; charset=utf-8",
		".json":  "application/json; charset=utf-8",
		".m3u":   "audio/mpegurl",
		".m4a":   "audio/mpeg",
		".md":    "text/markdown; charset=utf-8",
		".mjs":   "text/javascript; charset=utf-8",
		".mov":   "video/quicktime",
		".mp3":   "audio/mpeg",
		".mp4":   "video/mp4",
		".mpeg":  "video/mpeg",
		".mpg":   "video/mpeg",
		".ogg":   "audio/ogg",
		".ogv":   "video/ogg",
		".otf":   "font/otf",
		".pdf":   "application/pdf",
		".png":   "image/png",
		".qt":    "video/quicktime",
		".rar":   "application/rar",
		".rtf":   "application/rtf",
		".svg":   "image/svg+xml",
		".tar":   "application/x-tar",
		".tiff":  "image/tiff",
		".ts":    "video/MP2T",
		".ttc":   "font/collection",
		".ttf":   "font/ttf",
		".txt":   "text/plain; charset=utf-8",
		".wasm":  "application/wasm",
		".wav":   "audio/x-wav",
		".webm":  "video/webm",
		".webp":  "image/webp",
		".woff":  "font/woff",
		".woff2": "font/woff2",
		".xml":   "text/xml; charset=utf-8",
		".xz":    "application/x-xz",
		".yaml":  "application/yaml; charset=utf-8",
		".yml":   "application/yaml; charset=utf-8",
		".zip":   "application/zip",
	}
)
