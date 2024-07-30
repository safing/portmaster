package inspection

import (
	"sync"

	"github.com/safing/portmaster/service/network"
	"github.com/safing/portmaster/service/network/packet"
)

//nolint:golint,stylecheck
const (
	DO_NOTHING uint8 = iota
	BLOCK_PACKET
	DROP_PACKET
	BLOCK_CONN
	DROP_CONN
	STOP_INSPECTING
)

type inspectorFn func(*network.Connection, packet.Packet) uint8

var (
	inspectors      []inspectorFn
	inspectorNames  []string
	inspectVerdicts []network.Verdict
	inspectorsLock  sync.Mutex
)

// RegisterInspector registers a traffic inspector.
func RegisterInspector(name string, inspector inspectorFn, inspectVerdict network.Verdict) (index int) {
	inspectorsLock.Lock()
	defer inspectorsLock.Unlock()
	index = len(inspectors)
	inspectors = append(inspectors, inspector)
	inspectorNames = append(inspectorNames, name)
	inspectVerdicts = append(inspectVerdicts, inspectVerdict)
	return
}

// RunInspectors runs all the applicable inspectors on the given packet.
func RunInspectors(conn *network.Connection, pkt packet.Packet) (network.Verdict, bool) {
	// inspectorsLock.Lock()
	// defer inspectorsLock.Unlock()

	activeInspectors := conn.GetActiveInspectors()
	if activeInspectors == nil {
		activeInspectors = make([]bool, len(inspectors))
		conn.SetActiveInspectors(activeInspectors)
	}

	inspectorData := conn.GetInspectorData()
	if inspectorData == nil {
		inspectorData = make(map[uint8]interface{})
		conn.SetInspectorData(inspectorData)
	}

	continueInspection := false
	verdict := network.VerdictUndecided

	for key, skip := range activeInspectors {

		if skip {
			continue
		}

		// check if the active verdict is already past the inspection criteria.
		if conn.Verdict > inspectVerdicts[key] {
			activeInspectors[key] = true
			continue
		}

		action := inspectors[key](conn, pkt) // Actually run inspector
		switch action {
		case DO_NOTHING:
			if verdict < network.VerdictAccept {
				verdict = network.VerdictAccept
			}
			continueInspection = true
		case BLOCK_PACKET:
			if verdict < network.VerdictBlock {
				verdict = network.VerdictBlock
			}
			continueInspection = true
		case DROP_PACKET:
			verdict = network.VerdictDrop
			continueInspection = true
		case BLOCK_CONN:
			conn.SetVerdict(network.VerdictBlock, "", "", nil)
			verdict = conn.Verdict
			activeInspectors[key] = true
		case DROP_CONN:
			conn.SetVerdict(network.VerdictDrop, "", "", nil)
			verdict = conn.Verdict
			activeInspectors[key] = true
		case STOP_INSPECTING:
			activeInspectors[key] = true
		}

	}

	return verdict, continueInspection
}
