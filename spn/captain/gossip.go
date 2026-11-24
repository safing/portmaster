package captain

import (
	"sync"
)

var (
	gossipOps     = make(map[string]*GossipOp)
	gossipOpsLock sync.RWMutex
)

func registerGossipOp(craneID string, op *GossipOp) {
	gossipOpsLock.Lock()
	defer gossipOpsLock.Unlock()

	gossipOps[craneID] = op
}

func deleteGossipOp(craneID string) {
	gossipOpsLock.Lock()
	defer gossipOpsLock.Unlock()

	delete(gossipOps, craneID)
}

// gossipRelayMsg sends a gossip message to all gossip peers except
// receivedFrom. Pass an empty receivedFrom for the hub's own messages.
func gossipRelayMsg(receivedFrom string, msgType GossipMsgType, data []byte) {
	gossipOpsLock.RLock()
	defer gossipOpsLock.RUnlock()

	for craneID, gossipOp := range gossipOps {
		// Don't return same msg back to sender.
		if craneID == receivedFrom {
			continue
		}

		gossipOp.sendMsg(msgType, data)
	}
}
