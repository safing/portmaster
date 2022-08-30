package ui

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/spkg/zipfs"

	"github.com/safing/portbase/api"
	"github.com/safing/portbase/log"
	"github.com/safing/portbase/modules"
	"github.com/safing/portbase/updater"
	"github.com/safing/portmaster/updates"
)

var (
	apps     = make(map[string]*zipfs.FileSystem)
	appsLock sync.RWMutex
)

func registerRoutes() error {
	// Server assets.
	api.RegisterHandler(
		"/assets/{resPath:[a-zA-Z0-9/\\._-]+}",
		&archiveServer{defaultModuleName: "assets"},
	)

	// Add slash to plain module namespaces.
	api.RegisterHandler(
		"/ui/modules/{moduleName:[a-z]+}",
		api.WrapInAuthHandler(redirAddSlash, api.PermitAnyone, api.NotSupported),
	)

	// Serve modules.
	srv := &archiveServer{}
	api.RegisterHandler("/ui/modules/{moduleName:[a-z]+}/", srv)
	api.RegisterHandler("/ui/modules/{moduleName:[a-z]+}/{resPath:[a-zA-Z0-9/\\._-]+}", srv)

	// Redirect "/" to default module.
	api.RegisterHandler(
		"/",
		api.WrapInAuthHandler(redirectToDefault, api.PermitAnyone, api.NotSupported),
	)

	return nil
}

type archiveServer struct {
	defaultModuleName string
}

func (bs *archiveServer) BelongsTo() *modules.Module { return module }

func (bs *archiveServer) ReadPermission(*http.Request) api.Permission { return api.PermitAnyone }

func (bs *archiveServer) WritePermission(*http.Request) api.Permission { return api.NotSupported }

func (bs *archiveServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Get request context.
	ar := api.GetAPIRequest(r)
	if ar == nil {
		log.Errorf("ui: missing api request context")
		http.Error(w, "Internal server error.", http.StatusInternalServerError)
		return
	}

	moduleName, ok := ar.URLVars["moduleName"]
	if !ok {
		moduleName = bs.defaultModuleName
		if moduleName == "" {
			http.Error(w, "missing module name", http.StatusBadRequest)
			return
		}
	}

	resPath, ok := ar.URLVars["resPath"]
	if !ok || strings.HasSuffix(resPath, "/") {
		resPath = "index.html"
	}

	appsLock.RLock()
	archiveFS, ok := apps[moduleName]
	appsLock.RUnlock()
	if ok {
		ServeFileFromArchive(w, r, moduleName, archiveFS, resPath)
		return
	}

	// get file from update system
	zipFile, err := updates.GetFile(fmt.Sprintf("ui/modules/%s.zip", moduleName))
	if err != nil {
		if errors.Is(err, updater.ErrNotFound) {
			log.Tracef("ui: requested module %s does not exist", moduleName)
			http.Error(w, err.Error(), http.StatusNotFound)
		} else {
			log.Tracef("ui: error loading module %s: %s", moduleName, err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	// Open archive from disk.
	archiveFS, err = zipfs.New(zipFile.Path())
	if err != nil {
		log.Tracef("ui: error prepping module %s: %s", moduleName, err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	appsLock.Lock()
	apps[moduleName] = archiveFS
	appsLock.Unlock()

	ServeFileFromArchive(w, r, moduleName, archiveFS, resPath)
}

// ServeFileFromArchive serves a file from the given archive.
func ServeFileFromArchive(w http.ResponseWriter, r *http.Request, archiveName string, archiveFS *zipfs.FileSystem, path string) {
	readCloser, err := archiveFS.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			// Check if there is a base index.html file we can serve instead.
			var indexErr error
			path = "index.html"
			readCloser, indexErr = archiveFS.Open(path)
			if indexErr != nil {
				// If we cannot get an index, continue with handling the original error.
				log.Tracef("ui: requested resource \"%s\" not found in archive %s: %s", path, archiveName, err)
				http.Error(w, err.Error(), http.StatusNotFound)
				return
			}
		} else {
			log.Tracef("ui: error opening module %s: %s", archiveName, err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	// set content type
	_, ok := w.Header()["Content-Type"]
	if !ok {
		contentType := mimeTypeByExtension(filepath.Ext(path))
		if contentType != "" {
			w.Header().Set("Content-Type", contentType)
		}
	}

	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, err = io.Copy(w, readCloser)
		if err != nil {
			log.Errorf("ui: failed to serve file: %s", err)
			return
		}
	}

	_ = readCloser.Close()
}

// redirectToDefault redirects the request to the default UI module.
func redirectToDefault(w http.ResponseWriter, r *http.Request) {
	u, err := url.Parse("/ui/modules/portmaster/")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, r.URL.ResolveReference(u).String(), http.StatusTemporaryRedirect)
}

// redirAddSlash redirects the request to the same, but with a trailing slash.
func redirAddSlash(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, r.RequestURI+"/", http.StatusPermanentRedirect)
}

// We now do our mimetypes ourselves, because, as far as we analyzed, a Windows
// update screwed us over here and broke all the mime typing.
// (April 2021)

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
		".zip":   "application/zip",
	}
)

func mimeTypeByExtension(ext string) string {
	mimeType, ok := mimeTypes[ext]
	if ok {
		return mimeType
	}

	return defaultMimeType
}
