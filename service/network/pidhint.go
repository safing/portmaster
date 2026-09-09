package network

import (
	"sync"
	"time"

	"github.com/safing/portmaster/service/mgr"
	"github.com/safing/portmaster/service/network/packet"
	"github.com/safing/portmaster/service/process"
)

const (
	// pidHintGeneration defines the interval in which the pid hint cache is
	// rotated. Hints are held for one to two generations.
	pidHintGeneration = 1 * time.Second

	// pidHintMaxEntries defines how many hints the current generation may hold
	// before an early rotation is forced. This bounds memory usage during
	// bursts that are shorter than one generation.
	pidHintMaxEntries = 20000
)

// pidHints holds pid hints in two generations, so that expiry is a pointer swap
// instead of a scan or a timer per entry.
var pidHints = struct {
	lock sync.Mutex
	cur  map[string]int
	prev map[string]int
}{
	cur:  make(map[string]int),
	prev: make(map[string]int),
}

// SavePIDHint records the process attribution reported by an info-only packet.
//
// Info-only packets do not represent an actual packet and can never complete a
// connection, so they must not be tracked as connections. Keeping only the
// reported PID, and only for a short time, lets the first real packet of the
// connection skip process.GetPidOfConnection(), which has to scan the kernel
// socket tables to map the packet's 5-tuple to a socket and then to a process.
//
// The packet direction is not kept, as it is reported authoritatively by the
// interception hook of the real packet.
func SavePIDHint(pkt packet.Packet) {
	if !pkt.InfoOnly() {
		return
	}

	// Info-only packets are in use on this system, so expect them for new connections.
	infoOnlyPacketsActive.Set()

	info := pkt.Info()
	if info.PID == process.UndefinedProcessID {
		return
	}

	connID := pkt.GetConnectionID()

	pidHints.lock.Lock()
	defer pidHints.lock.Unlock()

	if len(pidHints.cur) >= pidHintMaxEntries {
		rotatePIDHints()
	}
	pidHints.cur[connID] = info.PID
}

// takePIDHint returns and removes the pid hint of the given connection ID.
func takePIDHint(connID string) (int, bool) {
	pidHints.lock.Lock()
	defer pidHints.lock.Unlock()

	if pid, ok := pidHints.cur[connID]; ok {
		delete(pidHints.cur, connID)
		return pid, true
	}
	if pid, ok := pidHints.prev[connID]; ok {
		delete(pidHints.prev, connID)
		return pid, true
	}

	return process.UndefinedProcessID, false
}

// rotatePIDHints drops the oldest generation of pid hints.
// The caller must hold pidHints.lock.
func rotatePIDHints() {
	pidHints.prev = pidHints.cur
	pidHints.cur = make(map[string]int)
}

func pidHintRotator(ctx *mgr.WorkerCtx) error {
	module.pidHintTicker = mgr.NewSleepyTicker(pidHintGeneration, 0)
	defer module.pidHintTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-module.pidHintTicker.Wait():
			pidHints.lock.Lock()
			rotatePIDHints()
			pidHints.lock.Unlock()
		}
	}
}
