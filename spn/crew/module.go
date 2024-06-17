package crew

import (
	"errors"
	"sync/atomic"
	"time"

	"github.com/safing/portmaster/service/mgr"
	"github.com/safing/portmaster/spn/terminal"
)

type Crew struct {
	mgr      *mgr.Manager
	instance instance
}

func (c *Crew) Start(m *mgr.Manager) error {
	c.mgr = m
	return start()
}

func (c *Crew) Stop(m *mgr.Manager) error {
	return stop()
}

func start() error {
	module.mgr.Repeat("sticky cleaner", 10*time.Minute, cleanStickyHubs)
	return registerMetrics()
}

func stop() error {
	clearStickyHubs()
	terminal.StopScheduler()

	return nil
}

var connectErrors = make(chan *terminal.Error, 10)

func reportConnectError(tErr *terminal.Error) {
	select {
	case connectErrors <- tErr:
	default:
	}
}

// ConnectErrors returns errors of connect operations.
// It only has a small and shared buffer and may only be used for indications,
// not for full monitoring.
func ConnectErrors() <-chan *terminal.Error {
	return connectErrors
}

var (
	module     *Crew
	shimLoaded atomic.Bool
)

// New returns a new Config module.
func New(instance instance) (*Crew, error) {
	if !shimLoaded.CompareAndSwap(false, true) {
		return nil, errors.New("only one instance allowed")
	}

	module = &Crew{
		instance: instance,
	}
	return module, nil
}

type instance interface{}
