package patrol

import (
	"errors"
	"sync/atomic"
	"time"

	"github.com/safing/portmaster/service/mgr"
	"github.com/safing/portmaster/spn/conf"
)

// ChangeSignalEventName is the name of the event that signals any change in the patrol system.
const ChangeSignalEventName = "change signal"

type Patrol struct {
	mgr      *mgr.Manager
	instance instance

	EventChangeSignal *mgr.EventMgr[struct{}]
}

func (p *Patrol) Start(m *mgr.Manager) error {
	p.mgr = m
	p.EventChangeSignal = mgr.NewEventMgr[struct{}](ChangeSignalEventName, m)

	if conf.PublicHub() {
		m.Repeat("connectivity test", 5*time.Minute, connectivityCheckTask)
	}
	return nil
}

func (p *Patrol) Stop(m *mgr.Manager) error {
	return nil
}

var (
	module     *Patrol
	shimLoaded atomic.Bool
)

// New returns a new Config module.
func New(instance instance) (*Patrol, error) {
	if !shimLoaded.CompareAndSwap(false, true) {
		return nil, errors.New("only one instance allowed")
	}

	module = &Patrol{
		instance: instance,
	}
	return module, nil
}

type instance interface{}
