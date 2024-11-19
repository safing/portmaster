//go:build !linux && !windows
// +build !linux,!windows

package dnslistener

type Listener struct{}

func newListener(module *DNSListener) (*Listener, error) {
	return &Listener{}, nil
}

func (l *Listener) flush() error {
	// Nothing to flush
	return nil
}

func (l *Listener) stop() error {
	return nil
}
