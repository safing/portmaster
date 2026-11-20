package network

import (
	"time"
)

// HasEnded reports whether the connection was marked as ended, without taking
// the connection lock.
func (conn *Connection) HasEnded() bool {
	return conn.ended.Load()
}

// RemoveEndedConnection removes an ended connection from the connection
// state before the cleaner does, because a new socket now uses its address
// tuple. The connection stays in the history. Returns false if the connection
// is locked by someone else or has not ended, in which case nothing is done.
//
// It is called from the packet dispatcher, which must not wait for a
// connection lock.
func RemoveEndedConnection(conn *Connection) bool {
	if !conn.TryLock() {
		return false
	}
	defer conn.Unlock()

	if conn.Ended == 0 {
		return false
	}
	conn.delete()
	return true
}

// EndConnectionForReusedTuple ends and removes the connection with the given
// ID, if there is one. Use it when the OS has handed the address tuple of the
// connection to a new socket, which proves that the old socket is gone. The
// connection stays in the history.
func EndConnectionForReusedTuple(connID string) {
	conn, ok := conns.get(connID)
	if !ok {
		return
	}

	conn.Lock()
	defer conn.Unlock()

	if conn.Ended == 0 {
		conn.Ended = time.Now().Unix()
		conn.ended.Store(true)
		conn.StopFirewallHandler()
		conn.Save()
	}
	conn.delete()
}
