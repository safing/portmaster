package helper

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/safing/jess/filesig"
	"github.com/safing/portmaster/base/updater"
)

// Release Channel Configuration Keys.
const (
	ReleaseChannelKey     = "core/releaseChannel"
	ReleaseChannelJSONKey = "core.releaseChannel"
)

// Release Channels.
const (
	ReleaseChannelStable  = "stable"
	ReleaseChannelBeta    = "beta"
	ReleaseChannelStaging = "staging"
	ReleaseChannelSupport = "support"
)

const jsonSuffix = ".json"

// SetIndexes sets the update registry indexes and also configures the registry
// to use pre-releases based on the channel.
func SetIndexes(
	registry *updater.ResourceRegistry,
	releaseChannel string,
	deleteUnusedIndexes bool,
	autoDownload bool,
	autoDownloadIntel bool,
) (warning error) {
	usePreReleases := false

	// Be reminded that the order is important, as indexes added later will
	// override the current release from earlier indexes.

	// Reset indexes before adding them (again).
	registry.ResetIndexes()

	// Add the intel index first, in order to be able to override it with the
	// other indexes when needed.
	registry.AddIndex(updater.Index{
		Path:         "all/intel/intel.json",
		AutoDownload: autoDownloadIntel,
	})

	// Always add the stable index as a base.
	registry.AddIndex(updater.Index{
		Path:         ReleaseChannelStable + jsonSuffix,
		AutoDownload: autoDownload,
	})

	// Add beta index if in beta or staging channel.
	indexPath := ReleaseChannelBeta + jsonSuffix
	if releaseChannel == ReleaseChannelBeta ||
		releaseChannel == ReleaseChannelStaging ||
		(releaseChannel == "" && indexExists(registry, indexPath)) {
		registry.AddIndex(updater.Index{
			Path:         indexPath,
			PreRelease:   true,
			AutoDownload: autoDownload,
		})
		usePreReleases = true
	} else if deleteUnusedIndexes {
		err := deleteIndex(registry, indexPath)
		if err != nil {
			warning = fmt.Errorf("failed to delete unused index %s: %w", indexPath, err)
		}
	}

	// Add staging index if in staging channel.
	indexPath = ReleaseChannelStaging + jsonSuffix
	if releaseChannel == ReleaseChannelStaging ||
		(releaseChannel == "" && indexExists(registry, indexPath)) {
		registry.AddIndex(updater.Index{
			Path:         indexPath,
			PreRelease:   true,
			AutoDownload: autoDownload,
		})
		usePreReleases = true
	} else if deleteUnusedIndexes {
		err := deleteIndex(registry, indexPath)
		if err != nil {
			warning = fmt.Errorf("failed to delete unused index %s: %w", indexPath, err)
		}
	}

	// Add support index if in support channel.
	indexPath = ReleaseChannelSupport + jsonSuffix
	if releaseChannel == ReleaseChannelSupport ||
		(releaseChannel == "" && indexExists(registry, indexPath)) {
		registry.AddIndex(updater.Index{
			Path:         indexPath,
			AutoDownload: autoDownload,
		})
		usePreReleases = true
	} else if deleteUnusedIndexes {
		err := deleteIndex(registry, indexPath)
		if err != nil {
			warning = fmt.Errorf("failed to delete unused index %s: %w", indexPath, err)
		}
	}

	// Set pre-release usage.
	registry.SetUsePreReleases(usePreReleases)

	return warning
}

func indexExists(registry *updater.ResourceRegistry, indexPath string) bool {
	_, err := os.Stat(filepath.Join(registry.StorageDir().Path, indexPath))
	return err == nil
}

func deleteIndex(registry *updater.ResourceRegistry, indexPath string) error {
	// Remove index itself.
	err := os.Remove(filepath.Join(registry.StorageDir().Path, indexPath))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}

	// Remove any accompanying signature.
	err = os.Remove(filepath.Join(registry.StorageDir().Path, indexPath+filesig.Extension))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}

	return nil
}
