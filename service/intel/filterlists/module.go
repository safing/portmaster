package filterlists

import (
	"errors"
	"fmt"
	"sync/atomic"

	"github.com/tevino/abool"

	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/service/mgr"
	"github.com/safing/portmaster/service/netenv"
	"github.com/safing/portmaster/service/updates"
)

const (
	filterlistsDisabled          = "filterlists:disabled"
	filterlistsUpdateFailed      = "filterlists:update-failed"
	filterlistsStaleDataSurvived = "filterlists:staledata"
)

type FilterLists struct {
	mgr      *mgr.Manager
	instance instance

	states *mgr.StateMgr
}

func (fl *FilterLists) Manager() *mgr.Manager {
	return fl.mgr
}

func (fl *FilterLists) States() *mgr.StateMgr {
	return fl.states
}

func (fl *FilterLists) Start() error {
	if err := prep(); err != nil {
		return err
	}
	return start()
}

func (fl *FilterLists) Stop() error {
	return stop()
}

// booleans mainly used to decouple the module
// during testing.
var (
	ignoreUpdateEvents = abool.New()
	ignoreNetEnvEvents = abool.New()
)

func init() {
	ignoreNetEnvEvents.Set()
}

func prep() error {
	module.instance.Updates().EventResourcesUpdated.AddCallback("Check for blocklist updates",
		func(wc *mgr.WorkerCtx, s struct{}) (bool, error) {
			if ignoreUpdateEvents.IsSet() {
				return false, nil
			}

			return false, tryListUpdate(wc.Ctx())
		})

	module.instance.NetEnv().EventOnlineStatusChange.AddCallback("Check for blocklist updates",
		func(wc *mgr.WorkerCtx, s netenv.OnlineStatus) (bool, error) {
			if ignoreNetEnvEvents.IsSet() {
				return false, nil
			}
			// Nothing to do if we went offline.
			if s == netenv.StatusOffline {
				return false, nil
			}

			return false, tryListUpdate(wc.Ctx())
		})

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
	module.states.Add(mgr.State{
		ID:      filterlistsDisabled,
		Name:    "Filter Lists Are Initializing",
		Message: "Filter lists are being downloaded and set up in the background. They will be activated as configured when finished.",
		Type:    mgr.StateTypeWarning,
	})
}

var (
	module     *FilterLists
	shimLoaded atomic.Bool
)

// New returns a new FilterLists module.
func New(instance instance) (*FilterLists, error) {
	if !shimLoaded.CompareAndSwap(false, true) {
		return nil, errors.New("only one instance allowed")
	}
	m := mgr.New("FilterLists")
	module = &FilterLists{
		mgr:      m,
		instance: instance,

		states: mgr.NewStateMgr(m),
	}
	return module, nil
}

type instance interface {
	Updates() *updates.Updates
	NetEnv() *netenv.NetEnv
}
