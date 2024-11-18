package dnslistener

import (
	"errors"
	"sync/atomic"

	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/service/mgr"
	"github.com/safing/portmaster/service/resolver"
)

var ResolverInfo = resolver.ResolverInfo{
	Name:   "SystemdResolver",
	Type:   "env",
	Source: "System",
}

type DNSListener struct {
	instance instance
	mgr      *mgr.Manager

	listener *Listener
}

func (dl *DNSListener) Manager() *mgr.Manager {
	return dl.mgr
}

func (dl *DNSListener) Start() error {
	var err error
	dl.listener, err = newListener(dl.mgr)
	if err != nil {
		log.Errorf("failed to start dns listener: %s", err)
	}

	return nil
}

func (dl *DNSListener) Stop() error {
	if dl.listener != nil {
		err := dl.listener.stop()
		if err != nil {
			log.Errorf("failed to close listener: %s", err)
		}
	}
	return nil
}

func (dl *DNSListener) Flush() error {
	return dl.listener.flish()
}

var shimLoaded atomic.Bool

func New(instance instance) (*DNSListener, error) {
	if !shimLoaded.CompareAndSwap(false, true) {
		return nil, errors.New("only one instance allowed")
	}
	m := mgr.New("DNSListener")
	module := &DNSListener{
		mgr:      m,
		instance: instance,
	}
	return module, nil
}

type instance interface{}
