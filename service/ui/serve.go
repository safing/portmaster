package ui

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/spkg/zipfs"

	"github.com/safing/portmaster/base/api"
	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/base/utils"
)

func (ui *UI) registerRoutes() error {
	// Server assets.
	api.RegisterHandler(
		"/assets/{resPath:[a-zA-Z0-9/\\._-]+}",
		&archiveServer{ui: ui, defaultModuleName: "assets"},
	)

	// Add slash to plain module namespaces.
	api.RegisterHandler(
		"/ui/modules/{moduleName:[a-z]+}",
		api.WrapInAuthHandler(redirAddSlash, api.PermitAnyone, api.NotSupported),
	)

	// Serve modules.
	srv := &archiveServer{ui: ui}
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
	ui                *UI
	defaultModuleName string
}

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

	archiveFS, ok := bs.ui.getArchive(moduleName)
	if ok {
		ServeFileFromArchive(w, r, moduleName, archiveFS, resPath)
		return
	}

	// Check if the upgrade lock is enabled.
	if bs.ui.upgradeLock.Load() {
		http.Error(w, "Resources locked, upgrade in progress.", http.StatusLocked)
		return
	}

	// get file from update system
	zipFile, err := bs.ui.instance.GetBinaryUpdateFile(fmt.Sprintf("%s.zip", moduleName))
	if err != nil {
		log.Tracef("ui: error loading module %s: %s", moduleName, err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Open archive from disk.
	archiveFS, err = zipfs.New(zipFile)
	if err != nil {
		log.Tracef("ui: error prepping module %s: %s", moduleName, err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	bs.ui.setArchive(moduleName, archiveFS)
	ServeFileFromArchive(w, r, moduleName, archiveFS, resPath)
}

// ServeFileFromArchive serves a file from the given archive.
func ServeFileFromArchive(w http.ResponseWriter, r *http.Request, archiveName string, archiveFS *zipfs.FileSystem, path string) {
	readCloser, err := archiveFS.Open(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
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
		contentType, _ := utils.MimeTypeByExtension(filepath.Ext(path))
		w.Header().Set("Content-Type", contentType)
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
