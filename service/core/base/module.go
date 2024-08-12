package base

import (
	"errors"
	"sync/atomic"

	"github.com/safing/portmaster/service/mgr"
)

<<<<<<< HEAD
type Base struct {
	mgr      *mgr.Manager
	instance instance
||||||| 151a548c
var module *modules.Module

func init() {
	module = modules.Register("base", nil, start, nil, "database", "config", "rng", "metrics")

	// For prettier subsystem graph, printed with --print-subsystem-graph
	/*
		subsystems.Register(
			"base",
			"Base",
			"THE GROUND.",
			baseModule,
			"",
			nil,
		)
	*/
=======
// Base is the base module.
type Base struct {
	mgr      *mgr.Manager
	instance instance
>>>>>>> develop
}

<<<<<<< HEAD
func (b *Base) Manager() *mgr.Manager {
	return b.mgr
}

func (b *Base) Start() error {
||||||| 151a548c
func start() error {
=======
// Manager returns the module manager.
func (b *Base) Manager() *mgr.Manager {
	return b.mgr
}

// Start starts the module.
func (b *Base) Start() error {
>>>>>>> develop
	startProfiling()
	registerLogCleaner()

	return nil
}
<<<<<<< HEAD

func (b *Base) Stop() error {
	return nil
}

var (
	module     *Base
	shimLoaded atomic.Bool
)

// New returns a new Base module.
func New(instance instance) (*Base, error) {
	if !shimLoaded.CompareAndSwap(false, true) {
		return nil, errors.New("only one instance allowed")
	}
	m := mgr.New("Base")
	module = &Base{
		mgr:      m,
		instance: instance,
	}

	if err := prep(instance); err != nil {
		return nil, err
	}
	if err := registerDatabases(); err != nil {
		return nil, err
	}

	return module, nil
}

type instance interface {
	SetCmdLineOperation(f func() error)
}
||||||| 151a548c
=======

// Stop stops the module.
func (b *Base) Stop() error {
	return nil
}

var (
	module     *Base
	shimLoaded atomic.Bool
)

// New returns a new Base module.
func New(instance instance) (*Base, error) {
	if !shimLoaded.CompareAndSwap(false, true) {
		return nil, errors.New("only one instance allowed")
	}
	m := mgr.New("Base")
	module = &Base{
		mgr:      m,
		instance: instance,
	}

	if err := prep(instance); err != nil {
		return nil, err
	}
	if err := registerDatabases(); err != nil {
		return nil, err
	}

	return module, nil
}

type instance interface {
	SetCmdLineOperation(f func() error)
}
>>>>>>> develop
