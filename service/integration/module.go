package integration

import (
	"errors"
	"sync/atomic"

	"github.com/safing/portmaster/service/mgr"
	"github.com/safing/portmaster/service/updates"
)

// OSIntegration module provides special integration with the OS.
type OSIntegration struct {
	m      *mgr.Manager
	states *mgr.StateMgr

	os OSSpecific

	instance instance
}

var shimLoaded atomic.Bool

// New returns a new OSIntegration module.
func New(instance instance) (*OSIntegration, error) {
	if !shimLoaded.CompareAndSwap(false, true) {
		return nil, errors.New("only one instance allowed")
	}

	m := mgr.New("OSIntegration")
	module := &OSIntegration{
		m:      m,
		states: m.NewStateMgr(),

		instance: instance,
	}

	return module, nil
}

// Manager returns the module manager.
func (i *OSIntegration) Manager() *mgr.Manager {
	return i.m
}

// Start starts the module.
func (i *OSIntegration) Start() error {
	return i.Initialize()
}

// Stop stops the module.
func (i *OSIntegration) Stop() error {
	return i.CleanUp()
}

type instance interface {
	Updates() *updates.Updates
}
