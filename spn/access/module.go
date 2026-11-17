// Package access has been stubbed out.
// The original access module handled Safing account authentication,
// SPN token management, and feature-flag checks against external servers.
// All of that functionality has been intentionally removed from portmasterpro.
package access

import (
	"github.com/safing/portmaster/service/mgr"
)

// Access is a no-op stub that satisfies the module interface without
// contacting any Safing authentication or account servers.
type Access struct {
	m *mgr.Manager
}

// New returns a no-op Access stub.
func New(_ any) (*Access, error) {
	return &Access{}, nil
}

// Manager returns nil — no manager needed for a no-op module.
func (a *Access) Manager() *mgr.Manager {
	return a.m
}

// Start is a no-op.
func (a *Access) Start() error {
	return nil
}

// Stop is a no-op.
func (a *Access) Stop() error {
	return nil
}

// IsLoggedIn always returns false — account login has been removed.
func (a *Access) IsLoggedIn() bool {
	return false
}

// HasFeature always returns true — all features are unlocked without an account.
func (a *Access) HasFeature(_ string) bool {
	return true
}
