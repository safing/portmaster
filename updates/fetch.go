package updates

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"time"

	"github.com/google/renameio"

	"github.com/Safing/portbase/log"
)

var (
	updateURLs = []string{
		"https://updates.safing.io",
	}
)

func fetchFile(realFilepath, updateFilepath string, tries int) error {
	// backoff when retrying
	if tries > 0 {
		time.Sleep(time.Duration(tries*tries) * time.Second)
	}

	// create URL
	downloadURL, err := joinURLandPath(updateURLs[tries%len(updateURLs)], updateFilepath)
	if err != nil {
		return fmt.Errorf("error build url (%s + %s): %s", updateURLs[tries%len(updateURLs)], updateFilepath, err)
	}

	// create destination dir
	dirPath := filepath.Dir(realFilepath)
	err = os.MkdirAll(dirPath, 0755)
	if err != nil {
		return fmt.Errorf("updates: could not create updates folder: %s", dirPath)
	}

	// open file for writing
	atomicFile, err := renameio.TempFile(filepath.Join(updateStoragePath, "tmp"), realFilepath)
	if err != nil {
		return fmt.Errorf("updates: could not create temp file for download: %s", err)
	}
	defer atomicFile.Cleanup()

	// start file download
	resp, err := http.Get(downloadURL)
	if err != nil {
		return fmt.Errorf("error fetching url (%s): %s", downloadURL, err)
	}
	defer resp.Body.Close()

	// download and write file
	n, err := io.Copy(atomicFile, resp.Body)
	if err != nil {
		return fmt.Errorf("failed downloading %s: %s", downloadURL, err)
	}
	if resp.ContentLength != n {
		return fmt.Errorf("download unfinished, written %d out of %d bytes.", n, resp.ContentLength)
	}

	// finalize file
	err = atomicFile.CloseAtomicallyReplace()
	if err != nil {
		return fmt.Errorf("updates: failed to finalize file %s: %s", realFilepath, err)
	}
	// set permissions
	err = os.Chmod(realFilepath, 0644)
	if err != nil {
		log.Warningf("updates: failed to set permissions on downloaded file %s: %s", realFilepath, err)
	}

	log.Infof("update: fetched %s (stored to %s)", downloadURL, realFilepath)
	return nil
}

func fetchData(downloadPath string, tries int) ([]byte, error) {
	// backoff when retrying
	if tries > 0 {
		time.Sleep(time.Duration(tries*tries) * time.Second)
	}

	// create URL
	downloadURL, err := joinURLandPath(updateURLs[tries%len(updateURLs)], downloadPath)
	if err != nil {
		return nil, fmt.Errorf("error build url (%s + %s): %s", updateURLs[tries%len(updateURLs)], downloadPath, err)
	}

	// start file download
	resp, err := http.Get(downloadURL)
	if err != nil {
		return nil, fmt.Errorf("error fetching url (%s): %s", downloadURL, err)
	}
	defer resp.Body.Close()

	// download and write file
	buf := bytes.NewBuffer(make([]byte, 0, resp.ContentLength))
	n, err := io.Copy(buf, resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed downloading %s: %s", downloadURL, err)
	}
	if resp.ContentLength != n {
		return nil, fmt.Errorf("download unfinished, written %d out of %d bytes.", n, resp.ContentLength)
	}

	return buf.Bytes(), nil
}

func joinURLandPath(baseURL, urlPath string) (string, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return "", err
	}

	u.Path = path.Join(u.Path, urlPath)
	return u.String(), nil
}
