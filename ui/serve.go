package ui

import (
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path"
	"path/filepath"
	"sync"

	resources "github.com/cookieo9/resources-go"
	"github.com/gorilla/mux"

	"github.com/Safing/portbase/api"
	"github.com/Safing/portbase/database"
	"github.com/Safing/portbase/log"
)

var (
	apps       = make(map[string]*resources.BundleSequence)
	appsLock   sync.RWMutex
	assets     *resources.BundleSequence
	assetsLock sync.RWMutex
)

func start() error {
	basePath := path.Join(database.GetDatabaseRoot(), "updates", "files", "apps")

	serveUIRouter := mux.NewRouter()
	serveUIRouter.HandleFunc("/assets/{resPath:[a-zA-Z0-9/\\._-]+}", ServeAssets(basePath))
	serveUIRouter.HandleFunc("/app/{appName:[a-z]+}/", ServeApps(basePath))
	serveUIRouter.HandleFunc("/app/{appName:[a-z]+}/{resPath:[a-zA-Z0-9/\\._-]+}", ServeApps(basePath))
	serveUIRouter.HandleFunc("/", RedirectToControl)

	api.RegisterAdditionalRoute("/assets/", serveUIRouter)
	api.RegisterAdditionalRoute("/app/", serveUIRouter)
	api.RegisterAdditionalRoute("/", serveUIRouter)

	return nil
}

// ServeApps serves app files.
func ServeApps(basePath string) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {

		vars := mux.Vars(r)
		appName, ok := vars["appName"]
		if !ok {
			http.Error(w, "missing app name", http.StatusBadRequest)
			return
		}

		resPath, ok := vars["resPath"]
		if !ok {
			http.Error(w, "missing resource path", http.StatusBadRequest)
			return
		}

		appsLock.RLock()
		bundle, ok := apps[appName]
		appsLock.RUnlock()
		if ok {
			ServeFileFromBundle(w, r, bundle, resPath)
			return
		}

		newBundle, err := resources.OpenZip(path.Join(basePath, fmt.Sprintf("%s.zip", appName)))
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		bundle = &resources.BundleSequence{newBundle}
		appsLock.Lock()
		apps[appName] = bundle
		appsLock.Unlock()

		ServeFileFromBundle(w, r, bundle, resPath)
	}
}

// ServeFileFromBundle serves a file from the given bundle.
func ServeFileFromBundle(w http.ResponseWriter, r *http.Request, bundle *resources.BundleSequence, path string) {
	readCloser, err := bundle.Open(path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	_, ok := w.Header()["Content-Type"]
	if !ok {
		contentType := mime.TypeByExtension(filepath.Ext(path))
		if contentType != "" {
			w.Header().Set("Content-Type", contentType)
		}
	}

	w.WriteHeader(http.StatusOK)
	if r.Method != "HEAD" {
		_, err = io.Copy(w, readCloser)
		if err != nil {
			log.Errorf("ui: failed to serve file: %s", err)
			return
		}
	}

	readCloser.Close()
	return
}

// ServeAssets serves global UI assets.
func ServeAssets(basePath string) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {

		vars := mux.Vars(r)
		resPath, ok := vars["resPath"]
		if !ok {
			http.Error(w, "missing resource path", http.StatusBadRequest)
			return
		}

		assetsLock.RLock()
		bundle := assets
		assetsLock.RUnlock()
		if bundle != nil {
			ServeFileFromBundle(w, r, bundle, resPath)
		}

		newBundle, err := resources.OpenZip(path.Join(basePath, "assets.zip"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		bundle = &resources.BundleSequence{newBundle}
		assetsLock.Lock()
		assets = bundle
		assetsLock.Unlock()

		ServeFileFromBundle(w, r, bundle, resPath)
	}
}

// RedirectToControl redirects the requests to the control app
func RedirectToControl(w http.ResponseWriter, r *http.Request) {
	u, err := url.Parse("/app/control")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, r.URL.ResolveReference(u).String(), http.StatusPermanentRedirect)
}
