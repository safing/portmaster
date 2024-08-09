package updater

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"

	"golang.org/x/exp/slices"

	"github.com/safing/jess/filesig"
	"github.com/safing/jess/lhash"
	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/base/utils"
)

// UpdateIndexes downloads all indexes. An error is only returned when all
// indexes fail to update.
func (reg *ResourceRegistry) UpdateIndexes(ctx context.Context) error {
	var lastErr error
	var anySuccess bool

	// Start registry operation.
	reg.state.StartOperation(StateChecking)
	defer reg.state.EndOperation()

	client := &http.Client{}
	for _, idx := range reg.getIndexes() {
		if err := reg.downloadIndex(ctx, client, idx); err != nil {
			lastErr = err
			log.Warningf("%s: failed to update index %s: %s", reg.Name, idx.Path, err)
		} else {
			anySuccess = true
		}
	}

	// If all indexes failed to update, fail.
	if !anySuccess {
		err := fmt.Errorf("failed to update all indexes, last error was: %w", lastErr)
		reg.state.ReportUpdateCheck(nil, err)
		return err
	}

	// Get pending resources and update status.
	pendingResourceVersions, _ := reg.GetPendingDownloads(true, false)
	reg.state.ReportUpdateCheck(
		humanInfoFromResourceVersions(pendingResourceVersions),
		nil,
	)

	return nil
}

func (reg *ResourceRegistry) downloadIndex(ctx context.Context, client *http.Client, idx *Index) error {
	var (
		// Index.
		indexErr    error
		indexData   []byte
		downloadURL string

		// Signature.
		sigErr       error
		verifiedHash *lhash.LabeledHash
		sigFileData  []byte
		verifOpts    = reg.GetVerificationOptions(idx.Path)
	)

	// Upgrade to v2 index if verification is enabled.
	downloadIndexPath := idx.Path
	if verifOpts != nil {
		downloadIndexPath = strings.TrimSuffix(downloadIndexPath, baseIndexExtension) + v2IndexExtension
	}

	// Download new index and signature.
	for tries := range 3 {
		// Index and signature need to be fetched together, so that they are
		// fetched from the same source. One source should always have a matching
		// index and signature. Backup sources may be behind a little.
		// If the signature verification fails, another source should be tried.

		// Get index data.
		indexData, downloadURL, indexErr = reg.fetchData(ctx, client, downloadIndexPath, tries)
		if indexErr != nil {
			log.Debugf("%s: failed to fetch index %s: %s", reg.Name, downloadURL, indexErr)
			continue
		}

		// Get signature and verify it.
		if verifOpts != nil {
			verifiedHash, sigFileData, sigErr = reg.fetchAndVerifySigFile(
				ctx, client,
				verifOpts, downloadIndexPath+filesig.Extension, nil,
				tries,
			)
			if sigErr != nil {
				log.Debugf("%s: failed to verify signature of %s: %s", reg.Name, downloadURL, sigErr)
				continue
			}

			// Check if the index matches the verified hash.
			if verifiedHash.Matches(indexData) {
				log.Infof("%s: verified signature of %s", reg.Name, downloadURL)
			} else {
				sigErr = ErrIndexChecksumMismatch
				log.Debugf("%s: checksum does not match file from %s", reg.Name, downloadURL)
				continue
			}
		}

		break
	}
	if indexErr != nil {
		return fmt.Errorf("failed to fetch index %s: %w", downloadIndexPath, indexErr)
	}
	if sigErr != nil {
		return fmt.Errorf("failed to fetch or verify index %s signature: %w", downloadIndexPath, sigErr)
	}

	// Parse the index file.
	indexFile, err := ParseIndexFile(indexData, idx.Channel, idx.LastRelease)
	if err != nil {
		return fmt.Errorf("failed to parse index %s: %w", idx.Path, err)
	}

	// Add index data to registry.
	if len(indexFile.Releases) > 0 {
		// Check if all resources are within the indexes' authority.
		authoritativePath := path.Dir(idx.Path) + "/"
		if authoritativePath == "./" {
			// Fix path for indexes at the storage root.
			authoritativePath = ""
		}
		cleanedData := make(map[string]string, len(indexFile.Releases))
		for key, version := range indexFile.Releases {
			if strings.HasPrefix(key, authoritativePath) {
				cleanedData[key] = version
			} else {
				log.Warningf("%s: index %s oversteps it's authority by defining version for %s", reg.Name, idx.Path, key)
			}
		}

		// add resources to registry
		err = reg.AddResources(cleanedData, idx, false, true, idx.PreRelease)
		if err != nil {
			log.Warningf("%s: failed to add resources: %s", reg.Name, err)
		}
	} else {
		log.Debugf("%s: index %s is empty", reg.Name, idx.Path)
	}

	// Check if dest dir exists.
	indexDir := filepath.FromSlash(path.Dir(idx.Path))
	err = reg.storageDir.EnsureRelPath(indexDir)
	if err != nil {
		log.Warningf("%s: failed to ensure directory for updated index %s: %s", reg.Name, idx.Path, err)
	}

	// Index files must be readable by portmaster-staert with user permissions in order to load the index.
	err = os.WriteFile( //nolint:gosec
		filepath.Join(reg.storageDir.Path, filepath.FromSlash(idx.Path)),
		indexData, 0o0644,
	)
	if err != nil {
		log.Warningf("%s: failed to save updated index %s: %s", reg.Name, idx.Path, err)
	}

	// Write signature file, if we have one.
	if len(sigFileData) > 0 {
		err = os.WriteFile( //nolint:gosec
			filepath.Join(reg.storageDir.Path, filepath.FromSlash(idx.Path)+filesig.Extension),
			sigFileData, 0o0644,
		)
		if err != nil {
			log.Warningf("%s: failed to save updated index signature %s: %s", reg.Name, idx.Path+filesig.Extension, err)
		}
	}

	log.Infof("%s: updated index %s with %d entries", reg.Name, idx.Path, len(indexFile.Releases))
	return nil
}

// DownloadUpdates checks if updates are available and downloads updates of used components.
func (reg *ResourceRegistry) DownloadUpdates(ctx context.Context, includeManual bool) error {
	// Start registry operation.
	reg.state.StartOperation(StateDownloading)
	defer reg.state.EndOperation()

	// Get pending updates.
	toUpdate, missingSigs := reg.GetPendingDownloads(includeManual, true)
	downloadDetailsResources := humanInfoFromResourceVersions(toUpdate)
	reg.state.UpdateOperationDetails(&StateDownloadingDetails{
		Resources: downloadDetailsResources,
	})

	// nothing to update
	if len(toUpdate) == 0 && len(missingSigs) == 0 {
		log.Infof("%s: everything up to date", reg.Name)
		return nil
	}

	// check download dir
	if err := reg.tmpDir.Ensure(); err != nil {
		return fmt.Errorf("could not prepare tmp directory for download: %w", err)
	}

	// download updates
	log.Infof("%s: starting to download %d updates", reg.Name, len(toUpdate))
	client := &http.Client{}
	var reportError error

	for i, rv := range toUpdate {
		log.Infof(
			"%s: downloading update [%d/%d]: %s version %s",
			reg.Name,
			i+1, len(toUpdate),
			rv.resource.Identifier, rv.VersionNumber,
		)
		var err error
		for tries := range 3 {
			err = reg.fetchFile(ctx, client, rv, tries)
			if err == nil {
				// Update resource version state.
				rv.resource.Lock()
				rv.Available = true
				if rv.resource.VerificationOptions != nil {
					rv.SigAvailable = true
				}
				rv.resource.Unlock()

				break
			}
		}
		if err != nil {
			reportError := fmt.Errorf("failed to download %s version %s: %w", rv.resource.Identifier, rv.VersionNumber, err)
			log.Warningf("%s: %s", reg.Name, reportError)
		}

		reg.state.UpdateOperationDetails(&StateDownloadingDetails{
			Resources:    downloadDetailsResources,
			FinishedUpTo: i + 1,
		})
	}

	if len(missingSigs) > 0 {
		log.Infof("%s: downloading %d missing signatures", reg.Name, len(missingSigs))

		for _, rv := range missingSigs {
			var err error
			for tries := range 3 {
				err = reg.fetchMissingSig(ctx, client, rv, tries)
				if err == nil {
					// Update resource version state.
					rv.resource.Lock()
					rv.SigAvailable = true
					rv.resource.Unlock()

					break
				}
			}
			if err != nil {
				reportError := fmt.Errorf("failed to download missing sig of %s version %s: %w", rv.resource.Identifier, rv.VersionNumber, err)
				log.Warningf("%s: %s", reg.Name, reportError)
			}
		}
	}

	reg.state.ReportDownloads(
		downloadDetailsResources,
		reportError,
	)
	log.Infof("%s: finished downloading updates", reg.Name)

	return nil
}

// DownloadUpdates checks if updates are available and downloads updates of used components.

// GetPendingDownloads returns the list of pending downloads.
// If manual is set, indexes with AutoDownload=false will be checked.
// If auto is set, indexes with AutoDownload=true will be checked.
func (reg *ResourceRegistry) GetPendingDownloads(manual, auto bool) (resources, sigs []*ResourceVersion) {
	reg.RLock()
	defer reg.RUnlock()

	// create list of downloads
	var toUpdate []*ResourceVersion
	var missingSigs []*ResourceVersion

	for _, res := range reg.resources {
		func() {
			res.Lock()
			defer res.Unlock()

			// Skip resources without index or indexes that should not be reported
			// according to parameters.
			switch {
			case res.Index == nil:
				// Cannot download if resource is not part of an index.
				return
			case manual && !res.Index.AutoDownload:
				// Manual update report and index is not auto-download.
			case auto && res.Index.AutoDownload:
				// Auto update report and index is auto-download.
			default:
				// Resource should not be reported.
				return
			}

			// Skip resources we don't need.
			switch {
			case res.inUse():
				// Update if resource is in use.
			case res.available():
				// Update if resource is available locally, ie. was used in the past.
			case utils.StringInSlice(reg.MandatoryUpdates, res.Identifier):
				// Update is set as mandatory.
			default:
				// Resource does not need to be updated.
				return
			}

			// Go through all versions until we find versions that need updating.
			for _, rv := range res.Versions {
				switch {
				case !rv.CurrentRelease:
					// We are not interested in older releases.
				case !rv.Available:
					// File not available locally, download!
					toUpdate = append(toUpdate, rv)
				case !rv.SigAvailable && res.VerificationOptions != nil:
					// File signature is not available and verification is enabled, download signature!
					missingSigs = append(missingSigs, rv)
				}
			}
		}()
	}

	slices.SortFunc(toUpdate, func(a, b *ResourceVersion) int {
		return strings.Compare(a.resource.Identifier, b.resource.Identifier)
	})
	slices.SortFunc(missingSigs, func(a, b *ResourceVersion) int {
		return strings.Compare(a.resource.Identifier, b.resource.Identifier)
	})

	return toUpdate, missingSigs
}

func humanInfoFromResourceVersions(resourceVersions []*ResourceVersion) []string {
	identifiers := make([]string, len(resourceVersions))

	for i, rv := range resourceVersions {
		identifiers[i] = fmt.Sprintf("%s v%s", rv.resource.Identifier, rv.VersionNumber)
	}

	return identifiers
}
