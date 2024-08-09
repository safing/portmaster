package broadcasts

import (
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/safing/portmaster/base/database"
	"github.com/safing/portmaster/service/mgr"
)

type Broadcasts struct {
	mgr      *mgr.Manager
	instance instance

	states *mgr.StateMgr
}

func (b *Broadcasts) Manager() *mgr.Manager {
	return b.mgr
}

func (b *Broadcasts) Start() error {
	return start()
}

func (b *Broadcasts) Stop() error {
	return nil
}

func (b *Broadcasts) States() *mgr.StateMgr {
	return b.states
}

var (
	db = database.NewInterface(&database.Options{
		Local:    true,
		Internal: true,
	})

	startOnce sync.Once
)

func init() {
	// module = modules.Register("broadcasts", prep, start, nil, "updates", "netenv", "notifications")
}

func prep() error {
	// Register API endpoints.
	if err := registerAPIEndpoints(); err != nil {
		return err
	}

	return nil
}

func start() error {
	// Ensure the install info is up to date.
	ensureInstallInfo()

	// Start broadcast notifier task.
	startOnce.Do(func() {
		module.mgr.Repeat("broadcast notifier", 10*time.Minute, broadcastNotify)
	})

	return nil
}

var (
	module     *Broadcasts
	shimLoaded atomic.Bool
)

// New returns a new Config module.
func New(instance instance) (*Broadcasts, error) {
	if !shimLoaded.CompareAndSwap(false, true) {
		return nil, errors.New("only one instance allowed")
	}
	m := mgr.New("Broadcasts")
	module = &Broadcasts{
		mgr:      m,
		instance: instance,
		states:   m.NewStateMgr(),
	}

	if err := prep(); err != nil {
		return nil, err
	}

	return module, nil
}

type instance interface{}
