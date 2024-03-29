package network

import (
	"strings"
	"sync"
)

type connectionStore struct {
	rw    sync.RWMutex
	items map[string]*Connection
}

func newConnectionStore() *connectionStore {
	return &connectionStore{
		items: make(map[string]*Connection, 100),
	}
}

func (cs *connectionStore) add(conn *Connection) {
	cs.rw.Lock()
	defer cs.rw.Unlock()

	cs.items[conn.ID] = conn
}

func (cs *connectionStore) delete(conn *Connection) {
	cs.rw.Lock()
	defer cs.rw.Unlock()

	delete(cs.items, conn.ID)
}

func (cs *connectionStore) get(id string) (*Connection, bool) {
	cs.rw.RLock()
	defer cs.rw.RUnlock()

	conn, ok := cs.items[id]
	return conn, ok
}

// findByPrefix returns the first connection where the key matches the given prefix.
// If the prefix matches multiple entries, the result is not deterministic.
func (cs *connectionStore) findByPrefix(prefix string) (*Connection, bool) { //nolint:unused
	cs.rw.RLock()
	defer cs.rw.RUnlock()

	for key, conn := range cs.items {
		if strings.HasPrefix(key, prefix) {
			return conn, true
		}
	}

	return nil, false
}

func (cs *connectionStore) clone() map[string]*Connection {
	cs.rw.RLock()
	defer cs.rw.RUnlock()

	m := make(map[string]*Connection, len(cs.items))
	for key, conn := range cs.items {
		m[key] = conn
	}
	return m
}

func (cs *connectionStore) list() []*Connection {
	cs.rw.RLock()
	defer cs.rw.RUnlock()

	l := make([]*Connection, 0, len(cs.items))
	for _, conn := range cs.items {
		l = append(l, conn)
	}
	return l
}

func (cs *connectionStore) len() int { //nolint:unused // TODO: Clean up if still unused.
	cs.rw.RLock()
	defer cs.rw.RUnlock()

	return len(cs.items)
}

func (cs *connectionStore) active() int {
	// Clone and count all active connections.
	var cnt int
	for _, conn := range cs.clone() {
		conn.Lock()
		if conn.Ended != 0 {
			cnt++
		}
		conn.Unlock()
	}

	return cnt
}
