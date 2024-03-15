package filterlists

import (
	"context"
	"fmt"

	"github.com/tevino/abool"

	"github.com/safing/portbase/log"
	"github.com/safing/portbase/modules"
	"github.com/safing/portmaster/service/netenv"
	"github.com/safing/portmaster/service/updates"
)

var module *modules.Module

const (
	filterlistsDisabled          = "filterlists:disabled"
	filterlistsUpdateFailed      = "filterlists:update-failed"
	filterlistsStaleDataSurvived = "filterlists:staledata"
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
		netenv.ModuleName,
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
		warnAboutDisabledFilterLists()
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

func warnAboutDisabledFilterLists() {
	module.Warning(
		filterlistsDisabled,
		"Filter Lists Are Initializing",
		"Filter lists are being downloaded and set up in the background. They will be activated as configured when finished.",
	)
}
