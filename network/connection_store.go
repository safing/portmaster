package network

import (
	"strconv"
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

func (cs *connectionStore) getID(conn *Connection) string {
	if conn.ID != "" {
		return conn.ID
	}
	return strconv.Itoa(conn.process.Pid) + "/" + conn.Scope
}

func (cs *connectionStore) add(conn *Connection) {
	cs.rw.Lock()
	defer cs.rw.Unlock()

	cs.items[cs.getID(conn)] = conn
}

func (cs *connectionStore) delete(conn *Connection) {
	cs.rw.Lock()
	defer cs.rw.Unlock()

	delete(cs.items, cs.getID(conn))
}

func (cs *connectionStore) get(id string) (*Connection, bool) {
	cs.rw.RLock()
	defer cs.rw.RUnlock()

	conn, ok := cs.items[id]
	return conn, ok
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
