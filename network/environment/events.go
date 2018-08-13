package environment

import (
	"sync"
)

var (
	networkChangedEventCh   = make(chan struct{}, 0)
	networkChangedEventLock sync.Mutex
)

func triggerNetworkChanged() {
	networkChangedEventLock.Lock()
	defer networkChangedEventLock.Unlock()
	close(networkChangedEventCh)
	networkChangedEventCh = make(chan struct{}, 0)
}

func NetworkChanged() <-chan struct{} {
	networkChangedEventLock.Lock()
	defer networkChangedEventLock.Unlock()
	return networkChangedEventCh
}
