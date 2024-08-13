package updates

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/safing/portmaster/base/log"
)

type UpdateIndex struct {
	Directory         string
	DownloadDirectory string
	Ignore            []string
	IndexURLs         []string
	IndexFile         string
	AutoApply         bool
}

func (ui *UpdateIndex) downloadIndexFile() (err error) {
	_ = os.MkdirAll(ui.Directory, defaultDirMode)
	_ = os.MkdirAll(ui.DownloadDirectory, defaultDirMode)
	for _, url := range ui.IndexURLs {
		err = ui.downloadIndexFileFromURL(url)
		if err != nil {
			log.Warningf("updates: %s", err)
			continue
		}
		// Downloading was successful.
		err = nil
		break
	}
	return
}

func (ui *UpdateIndex) checkForUpdates() (bool, error) {
	err := ui.downloadIndexFile()
	if err != nil {
		return false, err
	}

	currentBundle, err := ui.GetInstallBundle()
	if err != nil {
		return true, err // Current installed bundle not found, act as there is update.
	}
	updateBundle, err := ui.GetUpdateBundle()
	if err != nil {
		return false, err
	}

	return currentBundle.Version != updateBundle.Version, nil
}

func (ui *UpdateIndex) downloadIndexFileFromURL(url string) error {
	client := http.Client{}
	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("failed a get request to %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	filePath := fmt.Sprintf("%s/%s", ui.DownloadDirectory, ui.IndexFile)
	file, err := os.OpenFile(filePath, os.O_RDWR|os.O_CREATE, defaultFileMode)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()

	_, err = io.Copy(file, resp.Body)
	if err != nil {
		return err
	}

	return nil
}

func (ui *UpdateIndex) GetInstallBundle() (*Bundle, error) {
	indexFile := fmt.Sprintf("%s/%s", ui.Directory, ui.IndexFile)
	return ui.GetBundle(indexFile)
}

func (ui *UpdateIndex) GetUpdateBundle() (*Bundle, error) {
	indexFile := fmt.Sprintf("%s/%s", ui.DownloadDirectory, ui.IndexFile)
	return ui.GetBundle(indexFile)
}

func (ui *UpdateIndex) GetBundle(indexFile string) (*Bundle, error) {
	// Check if the file exists.
	file, err := os.Open(indexFile)
	if err != nil {
		return nil, fmt.Errorf("failed to open index file: %w", err)
	}
	defer func() { _ = file.Close() }()

	// Read
	content, err := io.ReadAll(file)
	if err != nil {
		return nil, err
	}

	// Parse
	var bundle Bundle
	err = json.Unmarshal(content, &bundle)
	if err != nil {
		return nil, err
	}

	return &bundle, nil
}
