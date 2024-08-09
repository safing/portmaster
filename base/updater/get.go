package updater

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/safing/portmaster/base/log"
)

// Errors returned by the updater package.
var (
	ErrNotFound                  = errors.New("the requested file could not be found")
	ErrNotAvailableLocally       = errors.New("the requested file is not available locally")
	ErrVerificationNotConfigured = errors.New("verification not configured for this resource")
)

// GetFile returns the selected (mostly newest) file with the given
// identifier or an error, if it fails.
func (reg *ResourceRegistry) GetFile(identifier string) (*File, error) {
	reg.RLock()
	res, ok := reg.resources[identifier]
	reg.RUnlock()
	if !ok {
		return nil, ErrNotFound
	}

	file := res.GetFile()
	// check if file is available locally
	if file.version.Available {
		file.markActiveWithLocking()

		// Verify file, if configured.
		_, err := file.Verify()
		if err != nil && !errors.Is(err, ErrVerificationNotConfigured) {
			// TODO: If verification is required, try deleting the resource and downloading it again.
			return nil, fmt.Errorf("failed to verify file: %w", err)
		}

		return file, nil
	}

	// check if online
	if !reg.Online {
		return nil, ErrNotAvailableLocally
	}

	// check download dir
	err := reg.tmpDir.Ensure()
	if err != nil {
		return nil, fmt.Errorf("could not prepare tmp directory for download: %w", err)
	}

	// Start registry operation.
	reg.state.StartOperation(StateFetching)
	defer reg.state.EndOperation()

	// download file
	log.Tracef("%s: starting download of %s", reg.Name, file.versionedPath)
	client := &http.Client{}
	for tries := range 5 {
		err = reg.fetchFile(context.TODO(), client, file.version, tries)
		if err != nil {
			log.Tracef("%s: failed to download %s: %s, retrying (%d)", reg.Name, file.versionedPath, err, tries+1)
		} else {
			file.markActiveWithLocking()

			// TODO: We just download the file - should we verify it again?
			return file, nil
		}
	}
	log.Warningf("%s: failed to download %s: %s", reg.Name, file.versionedPath, err)
	return nil, err
}

// GetVersion returns the selected version of the given identifier.
// The returned resource version may not be modified.
func (reg *ResourceRegistry) GetVersion(identifier string) (*ResourceVersion, error) {
	reg.RLock()
	res, ok := reg.resources[identifier]
	reg.RUnlock()
	if !ok {
		return nil, ErrNotFound
	}

	res.Lock()
	defer res.Unlock()

	return res.SelectedVersion, nil
}
