package network

import (
	"fmt"

	"github.com/google/gopacket"
)

type FlowDirection bool

// Keep in sync with reassembly.TCPFlowDirection
const (
	ClientToServer = false
	ServerToClient = true
)

func (dir FlowDirection) String() string {
	if !dir {
		return "client->server"
	}
	return "server->client"
}

func (dir FlowDirection) Reverse() FlowDirection { return !dir }

// VerdictReason is basically the same as endpoints.Reason but is
// duplicated here so packages don't need to depend on endpoints just
// for the Reason interface ...
type VerdictReason interface {
	// String should return a human readable string
	// describing the decision reason.
	String() string

	// Context returns the context that was used
	// for the decision.
	Context() interface{}
}

type PacketHandler interface {
	HandlePacket(conn *Connection, p gopacket.Packet) (Verdict, VerdictReason, error)
}

type StreamHandler interface {
	HandleStream(conn *Connection, dir FlowDirection, data []byte) (Verdict, VerdictReason, error)
}

type DgramHandler interface {
	HandleDGRAM(conn *Connection, dir FlowDirection, data []byte) (Verdict, VerdictReason, error)
}

func (conn *Connection) GetDPIState(key string) interface{} {
	conn.dpiStateLock.Lock()
	defer conn.dpiStateLock.Unlock()
	if conn.dpiState == nil {
		return nil
	}
	return conn.dpiState[key]
}

func (conn *Connection) SetDPIState(key string, val interface{}) {
	conn.dpiStateLock.Lock()
	defer conn.dpiStateLock.Unlock()
	if conn.dpiState == nil {
		conn.dpiState = make(map[string]interface{})
	}
	conn.dpiState[key] = val
}

// AddHandler
func (conn *Connection) AddHandler(handler interface{}) error {
	conn.handlerLock.Lock()
	defer conn.handlerLock.Unlock()

	added := false
	if ph, ok := handler.(PacketHandler); ok {
		added = true
		conn.packetHandlers = append(conn.packetHandlers, ph)
	}
	if sh, ok := handler.(StreamHandler); ok {
		added = true
		conn.streamHandlers = append(conn.streamHandlers, sh)
	}
	if dh, ok := handler.(DgramHandler); ok {
		added = true
		conn.dgramHandlers = append(conn.dgramHandlers, dh)
	}
	if !added {
		return fmt.Errorf("AddHandler called with invalid argument %T", handler)
	}
	return nil
}

func (conn *Connection) PacketHandlers() []PacketHandler {
	conn.handlerLock.RLock()
	defer conn.handlerLock.RUnlock()
	return conn.packetHandlers
}

func (conn *Connection) RemoveHandler(idx int, handler interface{}) error {
	conn.handlerLock.Lock()
	defer conn.handlerLock.Unlock()

	deleted := false
	if _, ok := handler.(PacketHandler); ok {
		deleted = true
		conn.packetHandlers[idx] = nil
	}
	if _, ok := handler.(StreamHandler); ok {
		deleted = true
		conn.streamHandlers[idx] = nil
	}
	if _, ok := handler.(DgramHandler); ok {
		deleted = true
		conn.dgramHandlers[idx] = nil
	}
	if !deleted {
		return fmt.Errorf("RemoveHandler called with invalid argument %T", handler)
	}
	return nil

}

func (conn *Connection) StreamHandlers() []StreamHandler {
	conn.handlerLock.RLock()
	defer conn.handlerLock.RUnlock()
	return conn.streamHandlers
}

func (conn *Connection) DgramHandlers() []DgramHandler {
	conn.handlerLock.RLock()
	defer conn.handlerLock.RUnlock()
	return conn.dgramHandlers
}
