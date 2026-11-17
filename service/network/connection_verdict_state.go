package network

// VerdictState is a consistent snapshot of the connection state that is
// needed to apply a verdict to a packet.
type VerdictState struct {
	// Verdict is the verdict of the connection.
	Verdict Verdict
	// VerdictPermanent is set if the verdict is handed to the OS integration
	// permanently.
	VerdictPermanent bool
	// Inbound is set if the connection is incoming.
	Inbound bool
	// PID is the PID of the owning process.
	PID int
}

// Layout of the packed verdict state:
//
//	bits  0-7:  Verdict
//	bit   8:    VerdictPermanent
//	bit   9:    Inbound
//	bit   10:   VerdictPermanent was set without the connection lock and is
//	            not yet applied to the VerdictPermanent field.
//	bits 16-63: PID (signed)
const (
	verdictStateVerdictMask      uint64 = 0xFF
	verdictStatePermanent        uint64 = 1 << 8
	verdictStateInbound          uint64 = 1 << 9
	verdictStatePermanentPending uint64 = 1 << 10
	verdictStatePIDShift                = 16
)

func packVerdictState(verdict Verdict, permanent, inbound bool, pid int) uint64 {
	packed := uint64(uint8(verdict)) | uint64(int64(pid))<<verdictStatePIDShift //nolint:gosec // Bit packing.
	if permanent {
		packed |= verdictStatePermanent
	}
	if inbound {
		packed |= verdictStateInbound
	}
	return packed
}

func unpackVerdictState(packed uint64) VerdictState {
	return VerdictState{
		Verdict:          Verdict(int8(uint8(packed & verdictStateVerdictMask))), //nolint:gosec // Bit packing.
		VerdictPermanent: packed&verdictStatePermanent != 0,
		Inbound:          packed&verdictStateInbound != 0,
		PID:              int(int64(packed) >> verdictStatePIDShift), //nolint:gosec // Bit packing, sign extending.
	}
}

// Lock locks the connection.
func (conn *Connection) Lock() {
	conn.Mutex.Lock()
	conn.applyPendingVerdictState()
}

// TryLock tries to lock the connection and reports whether it succeeded.
func (conn *Connection) TryLock() bool {
	if !conn.Mutex.TryLock() {
		return false
	}
	conn.applyPendingVerdictState()
	return true
}

// Unlock unlocks the connection and publishes the verdict state for packets
// that are handled without locking the connection, see VerdictState.
func (conn *Connection) Unlock() {
	conn.publishVerdictState()
	conn.Mutex.Unlock()
}

// VerdictState returns a consistent snapshot of the Verdict, VerdictPermanent,
// Inbound and PID fields without requiring the connection lock.
//
// Packets of connections without a firewall handler are handled directly on
// the packet dispatching goroutine, which must never wait for the connection
// lock. They use this snapshot instead of the fields, which may only be
// accessed while holding the connection lock.
//
// The snapshot is published whenever the connection is unlocked, and when the
// firewall handler is stopped. It never exposes a state from within a
// critical section, except for the one at the time the handler is stopped.
func (conn *Connection) VerdictState() VerdictState {
	return unpackVerdictState(conn.verdictState.Load())
}

// MakeVerdictPermanentLockFree marks the verdict of the connection as
// permanent without requiring the connection lock and returns the resulting
// verdict state. The VerdictPermanent field is updated the next time the
// connection is locked or unlocked.
func (conn *Connection) MakeVerdictPermanentLockFree() VerdictState {
	for {
		current := conn.verdictState.Load()
		updated := current | verdictStatePermanent | verdictStatePermanentPending
		if current == updated || conn.verdictState.CompareAndSwap(current, updated) {
			return unpackVerdictState(updated)
		}
	}
}

// applyPendingVerdictState applies changes to the verdict state that were
// made without the connection lock to the fields of the connection.
// The caller must hold the connection lock.
func (conn *Connection) applyPendingVerdictState() {
	if conn.verdictState.Load()&verdictStatePermanentPending != 0 {
		conn.VerdictPermanent = true
	}
}

// publishVerdictState publishes the current verdict state of the connection.
// The caller must hold the connection lock.
func (conn *Connection) publishVerdictState() {
	for {
		current := conn.verdictState.Load()
		if current&verdictStatePermanentPending != 0 {
			conn.VerdictPermanent = true
		}

		updated := packVerdictState(conn.Verdict, conn.VerdictPermanent, conn.Inbound, conn.PID)
		if current == updated || conn.verdictState.CompareAndSwap(current, updated) {
			return
		}
	}
}
