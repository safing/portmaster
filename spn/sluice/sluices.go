package sluice

import "sync"

var (
	sluices     = make(map[string]*Sluice)
	sluicesLock sync.RWMutex
)

func getSluice(network string) (s *Sluice, ok bool) {
	sluicesLock.RLock()
	defer sluicesLock.RUnlock()

	s, ok = sluices[network]
	return
}

func addSluice(s *Sluice) {
	sluicesLock.Lock()
	defer sluicesLock.Unlock()

	sluices[s.network] = s
}

func removeSluice(network string) {
	sluicesLock.Lock()
	defer sluicesLock.Unlock()

	delete(sluices, network)
}

func copySluices() map[string]*Sluice {
	sluicesLock.Lock()
	defer sluicesLock.Unlock()

	copied := make(map[string]*Sluice, len(sluices))
	for k, v := range sluices {
		copied[k] = v
	}
	return copied
}

func stopAllSluices() {
	for _, sluice := range copySluices() {
		sluice.abandon()
	}
}
