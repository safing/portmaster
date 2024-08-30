package registry

import (
	"fmt"
	"io"
	"net/http"
	"os"

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

func (ui *UpdateIndex) downloadIndexFile() (err error) {
	_ = os.MkdirAll(ui.DownloadDirectory, defaultDirMode)
	for _, url := range ui.IndexURLs {
		err = ui.downloadIndexFileFromURL(url)
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

func (ui *UpdateIndex) downloadIndexFileFromURL(url string) error {
	client := http.Client{}
	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("failed GET request to %s: %w", url, err)
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
