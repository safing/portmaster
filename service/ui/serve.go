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

// indexFile is the file served for directories and as the SPA fallback.
const indexFile = "index.html"

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
		resPath = indexFile
	}

	readCloser, servedPath, status, err := bs.ui.openArchiveFile(moduleName, resPath)
	switch {
	case errors.Is(err, errUpgradeInProgress):
		http.Error(w, "Resources locked, upgrade in progress.", status)
		return
	case err != nil:
		http.Error(w, err.Error(), status)
		return
	}
	defer func() {
		_ = readCloser.Close()
	}()

	serveFile(w, r, readCloser, servedPath)
}

var errUpgradeInProgress = errors.New("resources locked, upgrade in progress")

// openArchiveFile opens the requested file from the archive of the given
// module, loading the archive if needed.
// The file is opened while holding the archives lock, so that the archive
// cannot be closed by CloseArchives (eg. via EnableUpgradeLock) at the same
// time. Also, no archive is loaded or cached while the upgrade lock is enabled.
func (ui *UI) openArchiveFile(moduleName, resPath string) (file http.File, servedPath string, status int, err error) {
	// Fast path: archive is already loaded.
	ui.archivesLock.RLock()
	archiveFS, ok := ui.archives[moduleName]
	if ok {
		file, servedPath, status, err = openFileFromArchive(moduleName, archiveFS, resPath)
		ui.archivesLock.RUnlock()
		return file, servedPath, status, err
	}
	ui.archivesLock.RUnlock()

	// Slow path: load archive.

	// Check if the upgrade lock is enabled.
	if ui.upgradeLock.Load() {
		return nil, "", http.StatusLocked, errUpgradeInProgress
	}

	// Get file from update system.
	// Note: This must not be called while holding the archives lock, as the
	// updater holds its index lock while enabling the upgrade lock, which in
	// turn acquires the archives lock.
	zipFile, err := ui.instance.GetBinaryUpdateFile(fmt.Sprintf("%s.zip", moduleName))
	if err != nil {
		log.Tracef("ui: error loading module %s: %s", moduleName, err)
		return nil, "", http.StatusInternalServerError, err
	}

	// Open archive from disk.
	newArchiveFS, err := zipfs.New(zipFile)
	if err != nil {
		log.Tracef("ui: error prepping module %s: %s", moduleName, err)
		return nil, "", http.StatusInternalServerError, err
	}

	ui.archivesLock.Lock()
	defer ui.archivesLock.Unlock()

	switch archiveFS, ok = ui.archives[moduleName]; {
	case ui.upgradeLock.Load():
		// The upgrade lock was enabled in the meantime, do not keep the archive
		// open. This is checked while holding the archives lock, as
		// EnableUpgradeLock enables the lock before closing all archives.
		_ = newArchiveFS.Close()
		return nil, "", http.StatusLocked, errUpgradeInProgress
	case ok:
		// Another request loaded the archive in the meantime.
		_ = newArchiveFS.Close()
	default:
		archiveFS = newArchiveFS
		ui.archives[moduleName] = archiveFS
	}

	return openFileFromArchive(moduleName, archiveFS, resPath)
}

// ServeFileFromArchive serves a file from the given archive.
func ServeFileFromArchive(w http.ResponseWriter, r *http.Request, archiveName string, archiveFS *zipfs.FileSystem, path string) {
	readCloser, servedPath, status, err := openFileFromArchive(archiveName, archiveFS, path)
	if err != nil {
		http.Error(w, err.Error(), status)
		return
	}
	defer func() {
		_ = readCloser.Close()
	}()

	serveFile(w, r, readCloser, servedPath)
}

// openFileFromArchive opens the given path from the archive, falling back to
// the index.html if the path does not exist.
func openFileFromArchive(archiveName string, archiveFS *zipfs.FileSystem, path string) (file http.File, servedPath string, status int, err error) {
	readCloser, err := archiveFS.Open(path)
	if err == nil {
		// Directories cannot be served, handle them like missing files.
		info, statErr := readCloser.Stat()
		if statErr == nil && info.IsDir() {
			_ = readCloser.Close()
			err = fs.ErrNotExist
		}
	}
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			// Check if there is a base index.html file we can serve instead.
			var indexErr error
			readCloser, indexErr = archiveFS.Open(indexFile)
			if indexErr != nil {
				// If we cannot get an index, continue with handling the original error.
				log.Tracef("ui: requested resource \"%s\" not found in archive %s: %s", path, archiveName, err)
				return nil, "", http.StatusNotFound, err
			}
			return readCloser, indexFile, http.StatusOK, nil
		}

		log.Tracef("ui: error opening module %s: %s", archiveName, err)
		return nil, "", http.StatusInternalServerError, err
	}

	return readCloser, path, http.StatusOK, nil
}

// serveFile writes the given file to the response.
func serveFile(w http.ResponseWriter, r *http.Request, file io.Reader, path string) {
	// set content type
	_, ok := w.Header()["Content-Type"]
	if !ok {
		contentType, _ := utils.MimeTypeByExtension(filepath.Ext(path))
		w.Header().Set("Content-Type", contentType)
	}

	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, err := io.Copy(w, file)
		if err != nil {
			log.Errorf("ui: failed to serve file: %s", err)
			return
		}
	}
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
