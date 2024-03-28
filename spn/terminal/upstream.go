package terminal

import "time"

// Upstream defines the interface for upstream (parent) components.
type Upstream interface {
	Send(msg *Msg, timeout time.Duration) *Error
}

// UpstreamSendFunc is a helper to be able to satisfy the Upstream interface.
type UpstreamSendFunc func(msg *Msg, timeout time.Duration) *Error

// Send is used to send a message through this upstream.
func (fn UpstreamSendFunc) Send(msg *Msg, timeout time.Duration) *Error {
	return fn(msg, timeout)
}
