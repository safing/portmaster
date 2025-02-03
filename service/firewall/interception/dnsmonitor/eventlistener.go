//go:build !linux && !windows
// +build !linux,!windows

package dnsmonitor

type Listener struct{}

func newListener(_ *DNSMonitor) (*Listener, error) {
	return &Listener{}, nil
}

func (l *Listener) flush() error {
	// Nothing to flush
	return nil
}

func (l *Listener) stop() error {
	return nil
}
