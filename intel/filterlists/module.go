package filterlists

import (
	"context"
	"fmt"

	"github.com/safing/portbase/log"
	"github.com/safing/portbase/modules"
	"github.com/safing/portmaster/netenv"
	"github.com/safing/portmaster/updates"
	"github.com/tevino/abool"
)

var (
	module *modules.Module
)

const (
	filterlistsDisabled              = "filterlists:disabled"
	filterlistsUpdateFailed          = "filterlists:update-failed"
	filterlistsStaleDataSurvived     = "filterlists:staledata"
	filterlistsStaleDataDescr        = "Removing stale filter list records failed. Some connections may be overblocked."
	filterlistsUpdateInProgress      = "filterlists:update-in-progress"
	filterlistsUpdateInProgressDescr = "Performance slightly degraded during list update."
)

// booleans mainly used to decouple the module
// during testing.
var (
	ignoreUpdateEvents = abool.New()
	ignoreNetEnvEvents = abool.New()
)

func init() {
	ignoreNetEnvEvents.Set()

	module = modules.Register("filterlists", prep, start, stop, "base", "updates")
}

func prep() error {
	if err := module.RegisterEventHook(
		updates.ModuleName,
		updates.ResourceUpdateEvent,
		"Check for blocklist updates",
		func(ctx context.Context, _ interface{}) error {
			if ignoreUpdateEvents.IsSet() {
				return nil
			}

			return tryListUpdate(ctx)
		},
	); err != nil {
		return fmt.Errorf("failed to register resource update event handler: %w", err)
	}

	if err := module.RegisterEventHook(
		"netenv",
		netenv.OnlineStatusChangedEvent,
		"Check for blocklist updates",
		func(ctx context.Context, _ interface{}) error {
			if ignoreNetEnvEvents.IsSet() {
				return nil
			}

			// Nothing to do if we went offline.
			if !netenv.Online() {
				return nil
			}

			return tryListUpdate(ctx)
		},
	); err != nil {
		return fmt.Errorf("failed to register online status changed event handler: %w", err)
	}

	return nil
}

func start() error {
	filterListLock.Lock()
	defer filterListLock.Unlock()

	ver, err := getCacheDatabaseVersion()
	if err == nil {
		log.Debugf("intel/filterlists: cache database has version %s", ver.String())

		if err = defaultFilter.loadFromCache(); err != nil {
			err = fmt.Errorf("failed to initialize bloom filters: %w", err)
		}
	}

	if err != nil {
		log.Debugf("intel/filterlists: blocklists disabled, waiting for update (%s)", err)
		module.Warning(filterlistsDisabled, "Blocklist features disabled, waiting for update")
	} else {
		log.Debugf("intel/filterlists: using cache database")
		close(filterListsLoaded)
	}

	return nil
}

func stop() error {
	filterListsLoaded = make(chan struct{})
	return nil
}
