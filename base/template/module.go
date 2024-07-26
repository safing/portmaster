package template

import (
	"time"

	"github.com/safing/portmaster/base/config"
	"github.com/safing/portmaster/service/mgr"
)

// Template showcases the usage of the module system.
type Template struct {
	i      instance
	m      *mgr.Manager
	states *mgr.StateMgr

	EventRecordAdded   *mgr.EventMgr[string]
	EventRecordDeleted *mgr.EventMgr[string]

	specialWorkerMgr *mgr.WorkerMgr
}

type instance interface{}

// New returns a new template.
func New(instance instance) (*Template, error) {
	m := mgr.New("template")
	t := &Template{
		i:      instance,
		m:      m,
		states: m.NewStateMgr(),

		EventRecordAdded:   mgr.NewEventMgr[string]("record added", m),
		EventRecordDeleted: mgr.NewEventMgr[string]("record deleted", m),

		specialWorkerMgr: m.NewWorkerMgr("special worker", serviceWorker, nil),
	}

	// register options
	err := config.Register(&config.Option{
		Name:            "language",
		Key:             "template/language",
		Description:     "Sets the language for the template [TEMPLATE]",
		OptType:         config.OptTypeString,
		ExpertiseLevel:  config.ExpertiseLevelUser, // default
		ReleaseLevel:    config.ReleaseLevelStable, // default
		RequiresRestart: false,                     // default
		DefaultValue:    "en",
		ValidationRegex: "^[a-z]{2}$",
	})
	if err != nil {
		return nil, err
	}

	return t, nil
}

// Manager returns the module manager.
func (t *Template) Manager() *mgr.Manager {
	return t.m
}

// States returns the module states.
func (t *Template) States() *mgr.StateMgr {
	return t.states
}

// Start starts the module.
func (t *Template) Start() error {
	t.m.Go("worker", serviceWorker)
	t.specialWorkerMgr.Delay(10 * time.Minute)

	return nil
}

// Stop stops the module.
func Stop() error {
	return nil
}

func serviceWorker(w *mgr.WorkerCtx) error {
	for {
		select {
		case <-time.After(1 * time.Second):
			err := do()
			if err != nil {
				return err
			}
		case <-w.Done():
			return nil
		}
	}
}

func do() error {
	return nil
}
