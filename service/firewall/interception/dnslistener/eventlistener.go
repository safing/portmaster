//go:build !linux && !windows
// +build !linux,!windows

package dnslistener

import (
	"github.com/safing/portmaster/service/mgr"
)

type Listener struct{}

func newListener(_ *mgr.Manager) (*Listener, error) {
	return &Listener{}, nil
}

func (l *Listener) flish() error {
	// Nothing to flush
	return nil
}

func (l *Listener) stop() error {
	return nil
}
