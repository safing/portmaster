package ui

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"sync"

	resources "github.com/cookieo9/resources-go"

	"github.com/safing/portbase/api"
	"github.com/safing/portbase/log"
	"github.com/safing/portbase/updater"
	"github.com/safing/portmaster/updates"
)

var (
	apps     = make(map[string]*resources.BundleSequence)
	appsLock sync.RWMutex
)

func registerRoutes() error {
	// Server assets.
	api.RegisterHandler(
		"/assets/{resPath:[a-zA-Z0-9/\\._-]+}",
		&bundleServer{defaultModuleName: "assets"},
	)

	// Add slash to plain module namespaces.
	api.RegisterHandler(
		"/ui/modules/{moduleName:[a-z]+}",
		api.WrapInAuthHandler(redirAddSlash, api.PermitAnyone, api.NotSupported),
	)

	// Serve modules.
	srv := &bundleServer{}
	api.RegisterHandler("/ui/modules/{moduleName:[a-z]+}/", srv)
	api.RegisterHandler("/ui/modules/{moduleName:[a-z]+}/{resPath:[a-zA-Z0-9/\\._-]+}", srv)

	// Redirect "/" to default module.
	api.RegisterHandler(
		"/",
		api.WrapInAuthHandler(redirectToDefault, api.PermitAnyone, api.NotSupported),
	)

	return nil
}

type bundleServer struct {
	defaultModuleName string
}

func (bs *bundleServer) ReadPermission(*http.Request) api.Permission { return api.PermitAnyone }

func (bs *bundleServer) WritePermission(*http.Request) api.Permission { return api.NotSupported }

func (bs *bundleServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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
	bundle, ok := apps[moduleName]
	appsLock.RUnlock()
	if ok {
		ServeFileFromBundle(w, r, moduleName, bundle, resPath)
		return
	}

	// get file from update system
	zipFile, err := updates.GetFile(fmt.Sprintf("ui/modules/%s.zip", moduleName))
	if err != nil {
		if err == updater.ErrNotFound {
			log.Tracef("ui: requested module %s does not exist", moduleName)
			http.Error(w, err.Error(), http.StatusNotFound)
		} else {
			log.Tracef("ui: error loading module %s: %s", moduleName, err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	// open bundle
	newBundle, err := resources.OpenZip(zipFile.Path())
	if err != nil {
		log.Tracef("ui: error prepping module %s: %s", moduleName, err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	bundle = &resources.BundleSequence{newBundle}
	appsLock.Lock()
	apps[moduleName] = bundle
	appsLock.Unlock()

	ServeFileFromBundle(w, r, moduleName, bundle, resPath)
}

// ServeFileFromBundle serves a file from the given bundle.
func ServeFileFromBundle(w http.ResponseWriter, r *http.Request, bundleName string, bundle *resources.BundleSequence, path string) {
	readCloser, err := bundle.Open(path)
	if err != nil {
		if err == resources.ErrNotFound {
			// Check if there is a base index.html file we can serve instead.
			var indexErr error
			path = "index.html"
			readCloser, indexErr = bundle.Open(path)
			if indexErr != nil {
				// If we cannot get an index, continue with handling the original error.
				log.Tracef("ui: requested resource \"%s\" not found in bundle %s: %s", path, bundleName, err)
				http.Error(w, err.Error(), http.StatusNotFound)
				return
			}
		} else {
			log.Tracef("ui: error opening module %s: %s", bundleName, err)
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

	// TODO: Set content security policy
	// For some reason, this breaks the ui client
	// w.Header().Set("Content-Security-Policy", "default-src 'self'")

	w.WriteHeader(http.StatusOK)
	if r.Method != "HEAD" {
		_, err = io.Copy(w, readCloser)
		if err != nil {
			log.Errorf("ui: failed to serve file: %s", err)
			return
		}
	}

	readCloser.Close()
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
		".json":  "application/json",
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
