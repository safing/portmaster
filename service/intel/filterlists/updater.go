package filterlists

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/hashicorp/go-version"
	"github.com/tevino/abool"

	"github.com/safing/portmaster/base/database"
	"github.com/safing/portmaster/base/database/query"
	"github.com/safing/portmaster/base/database/storage"
	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/service/mgr"
	"github.com/safing/portmaster/service/updates"
)

var updateInProgress = abool.New()

// obsoleteEntriesCutoffMargin is subtracted from the update start time when
// purging obsolete entries after a base update. It tolerates small backward
// clock corrections during the update, which would otherwise make entries
// written by the update itself look obsolete.
const obsoleteEntriesCutoffMargin = time.Minute

// tryListUpdate wraps performUpdate but ensures the module's
// error state is correctly set or resolved.
func tryListUpdate(ctx context.Context) error {
	err := performUpdate(ctx)
	if err != nil {
		// Check if we are shutting down, as to not raise a false alarm.
		if module.mgr.IsDone() {
			return nil
		}

		// Check if the module already has a failure status set. If not, set a
		// generic one with the returned error.

		hasWarningState := false
		for _, state := range module.states.Export().States {
			if state.Type == mgr.StateTypeWarning {
				hasWarningState = true
			}
		}
		if !hasWarningState {
			module.states.Add(mgr.State{
				ID:      filterlistsUpdateFailed,
				Name:    "Filter Lists Update Failed",
				Message: fmt.Sprintf("The Portmaster failed to process a filter lists update. Filtering capabilities are currently either impaired or not available at all. Error: %s", err.Error()),
				Type:    mgr.StateTypeWarning,
			})
		}

		return err
	}

	// Clear the failure state on any successful update check.
	module.states.Remove(filterlistsUpdateFailed)
	return nil
}

func performUpdate(ctx context.Context) error {
	if !updateInProgress.SetToIf(false, true) {
		log.Debugf("intel/filterlists: upgrade already in progress")
		return nil
	}
	defer updateInProgress.UnSet()

	// First, update the list index.
	err := updateListIndex()
	if err != nil {
		log.Warningf("intel/filterlists: failed update list index: %s", err)
	}

	upgradables, err := getUpgradableFiles()
	if err != nil {
		return err
	}
	log.Debugf("intel/filterlists: resources to update: %v", upgradables)

	if len(upgradables) == 0 {
		log.Debugf("intel/filterlists: ignoring update, latest version is already used")
		return nil
	}

	cleanupRequired := false
	filterToUpdate := defaultFilter

	// Every entry written by this update has a newer timestamp than this.
	// The cleanup uses it to tell obsolete entries from current ones,
	// regardless of how long processing takes (slow disk, system sleep).
	updateStartedAt := time.Now()

	// perform the actual upgrade by processing each file
	// in the returned order.
	for idx, file := range upgradables {
		log.Debugf("intel/filterlists: applying update (%d) %s version %s", idx, file.Filename, file.Version)

		if isBaseFile(file) {
			if idx != 0 {
				log.Warningf("intel/filterlists: upgrade order is wrong, base file needs to be updated first not at idx %d", idx)
				// we still continue because after processing the base
				// file everything is correct again, we just used some
				// CPU and IO resources for nothing when processing
				// the previous files.
			}
			cleanupRequired = true

			// since we are processing a base update we will create our
			// bloom filters from scratch.
			filterToUpdate = newScopedBloom()
		}

		if err := processListFile(ctx, filterToUpdate, file); err != nil {
			return fmt.Errorf("failed to process upgrade %s version %s: %w", file.Filename, file.Version, err)
		}
	}

	if filterToUpdate != defaultFilter {
		log.Infof("intel/filterlists: rebuilt bloom filters (added/capacity): %s", filterToUpdate.capacityReport())

		// replace the bloom filters in our default
		// filter.
		defaultFilter.replaceWith(filterToUpdate)
	}

	// from now on, the database is ready and can be used if
	// it wasn't loaded yet.
	if !isLoaded() {
		close(filterListsLoaded)
	}

	if err := defaultFilter.saveToCache(); err != nil {
		// just handle the error by logging as it's only consequence
		// is that we will need to reprocess all files during the next
		// start.
		log.Errorf("intel/filterlists: failed to persist bloom filters in cache database: %s", err)
	}

	// if we processed the base file we need to perform
	// some cleanup on filterlists entities that have not
	// been updated now. Once we are done, start a worker
	// for that purpose.
	if cleanupRequired {
		purgeBefore := updateStartedAt.Add(-obsoleteEntriesCutoffMargin)
		err = module.mgr.Do("filterlists:cleanup", func(wc *mgr.WorkerCtx) error {
			return removeAllObsoleteFilterEntries(wc, purgeBefore)
		})
		if err != nil {
			// if we failed to remove all stale cache entries
			// we abort now WITHOUT updating the database version. This means
			// we'll try again during the next update.
			module.states.Add(mgr.State{
				ID:      filterlistsStaleDataSurvived,
				Name:    "Filter Lists May Overblock",
				Message: fmt.Sprintf("The Portmaster failed to delete outdated filter list data. Filtering capabilities are fully available, but overblocking may occur. Error: %s", err.Error()), //nolint:misspell // overblocking != overclocking
				Type:    mgr.StateTypeWarning,
			})
			return fmt.Errorf("failed to cleanup stale cache records: %w", err)
		}
	}

	// try to save the highest version of our files.
	highestVersion := upgradables[len(upgradables)-1]
	if err := setCacheDatabaseVersion(highestVersion.Version); err != nil {
		log.Errorf("intel/filterlists: failed to save cache database version: %s", err)
	} else {
		log.Infof("intel/filterlists: successfully migrated cache database to %s", highestVersion.Version)
	}

	// The list update succeeded, resolve any states.
	module.states.Clear()
	return nil
}

// removeAllObsoleteFilterEntries deletes all filter list entries that were
// last written before purgeBefore.
func removeAllObsoleteFilterEntries(wc *mgr.WorkerCtx, purgeBefore time.Time) error {
	log.Debugf("intel/filterlists: cleanup task started, removing obsolete filter list entries written before %s ...", purgeBefore)

	// First try to purge with PurgeOlderThan.
	n, err := cache.PurgeOlderThan(wc.Ctx(), filterListKeyPrefix, purgeBefore)
	switch {
	case err == nil:
		// Success!
		log.Debugf("intel/filterlists: successfully removed %d obsolete entries", n)
		return nil
	case errors.Is(err, database.ErrNotImplemented) || errors.Is(err, storage.ErrNotImplemented):
		// Try next method.
	default:
		// Return error.
		return err
	}

	// Try with regular purge.
	n, err = cache.Purge(wc.Ctx(), query.New(filterListKeyPrefix).Where(
		query.Where("UpdatedAt", query.LessThan, purgeBefore.Unix()),
	))
	if err != nil {
		return err
	}

	log.Debugf("intel/filterlists: successfully removed %d obsolete entries", n)
	return nil
}

// isBaseFile reports whether file is the base filter list file.
// Artifacts are compared by filename, as the updater returns a
// new Artifact instance on every GetFile call.
func isBaseFile(file *updates.Artifact) bool {
	return file.Filename == baseListFilePath
}

// getUpgradableFiles returns a slice of filterlists files
// that should be updated. The files MUST be updated and
// processed in the returned order!
func getUpgradableFiles() ([]*updates.Artifact, error) {
	var updateOrder []*updates.Artifact

	// All available files are candidates. Which of them actually need to be
	// processed is decided by resolveUpdateOrder based on the cache database
	// version.
	for _, filename := range []string{baseListFilePath, intermediateListFilePath, urgentListFilePath} {
		file, err := module.instance.IntelUpdates().GetFile(filename)
		if err != nil {
			log.Warningf("intel/filterlists: failed to get %s update: %s", filename, err)
			continue
		}
		if file.SemVer() == nil {
			log.Warningf("intel/filterlists: %s has version %s, but semver is invalid", filename, file.Version)
			continue
		}
		updateOrder = append(updateOrder, file)
	}

	return resolveUpdateOrder(updateOrder, getCacheDatabaseVersionOrZero())
}

// getCacheDatabaseVersionOrZero returns the version the cache database was
// last updated to, or v0.0.0 if the filter lists are not loaded or the
// version is unknown.
func getCacheDatabaseVersionOrZero() *version.Version {
	zeroVersion, _ := version.NewSemver("v0.0.0")

	if !isLoaded() {
		return zeroVersion
	}

	cacheDBVersion, err := getCacheDatabaseVersion()
	if err != nil {
		if !errors.Is(err, database.ErrNotFound) {
			log.Errorf("intel/filterlists: failed to get cache database version: %s", err)
		}
		return zeroVersion
	}

	return cacheDBVersion
}

// resolveUpdateOrder sorts updateOrder by ascending version and returns the
// files that need to be applied on top of a cache database at cacheDBVersion,
// in the order they must be processed. If a base file newer than
// cacheDBVersion is available, processing starts with it and older files are
// skipped, as the base file already contains their changes.
func resolveUpdateOrder(updateOrder []*updates.Artifact, cacheDBVersion *version.Version) ([]*updates.Artifact, error) {
	// sort the update order by ascending version
	sort.Sort(byAscVersion(updateOrder))
	log.Tracef("intel/filterlists: order of potential updates: %v", updateOrder)

	startAtIdx := -1
	for idx, file := range updateOrder {
		log.Tracef("intel/filterlists: checking file with version %s against %s", file.SemVer(), cacheDBVersion)
		if file.SemVer().GreaterThan(cacheDBVersion) && (startAtIdx == -1 || isBaseFile(file)) {
			startAtIdx = idx
		}
	}

	// if startAtIdx == -1 we don't have any upgradables to
	// process.
	if startAtIdx == -1 {
		log.Tracef("intel/filterlists: nothing to process, latest version %s already in use", cacheDBVersion)
		return nil, nil
	}

	// skip any files that are lower then the current cache db version
	// or after which a base upgrade would be performed.
	return updateOrder[startAtIdx:], nil
}

type byAscVersion []*updates.Artifact

func (fs byAscVersion) Len() int { return len(fs) }

func (fs byAscVersion) Less(i, j int) bool {
	return fs[i].SemVer().LessThan(fs[j].SemVer())
}

func (fs byAscVersion) Swap(i, j int) {
	fi := fs[i]
	fj := fs[j]

	fs[i] = fj
	fs[j] = fi
}
