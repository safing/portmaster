package integration

import (
	"github.com/safing/portmaster/service/mgr"
	"github.com/safing/portmaster/service/updates"
)

// OSIntegration module provides special integration with the OS.
type OSIntegration struct {
	m *mgr.Manager

	OnInitializedEvent *mgr.EventMgr[struct{}]

	//nolint:unused
	os OSSpecific

	instance instance
}

// New returns a new OSIntegration module.
func New(instance instance) (*OSIntegration, error) {
	m := mgr.New("OSIntegration")
	module := &OSIntegration{
		m:                  m,
		OnInitializedEvent: mgr.NewEventMgr[struct{}]("on-initialized", m),
		instance:           instance,
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
