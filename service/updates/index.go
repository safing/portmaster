package updates

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/safing/portmaster/base/log"
)

type UpdateIndex struct {
	Directory         string
	DownloadDirectory string
	PurgeDirectory    string
	Ignore            []string
	IndexURLs         []string
	IndexFile         string
	AutoApply         bool
}

func (ui *UpdateIndex) DownloadIndexFile(client *http.Client) (err error) {
	// Make sure dir exists
	_ = os.MkdirAll(ui.DownloadDirectory, defaultDirMode)
	for _, url := range ui.IndexURLs {
		err = ui.downloadIndexFileFromURL(client, url)
		if err != nil {
			log.Warningf("updates: failed while downloading index file %s", err)
			continue
		}
		// Downloading was successful.
		err = nil
		break
	}
	return
}

func (ui *UpdateIndex) downloadIndexFileFromURL(client *http.Client, url string) error {
	// Request the index file
	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("failed GET request to %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Check the status code
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("received error from the server status code: %s", resp.Status)
	}
	// Create file
	filePath := filepath.Join(ui.DownloadDirectory, ui.IndexFile)
	file, err := os.Create(filePath)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()

	// Write body of the response
	_, err = io.Copy(file, resp.Body)
	if err != nil {
		return err
	}

	return nil
}
