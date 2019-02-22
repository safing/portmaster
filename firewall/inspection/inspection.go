// Copyright Safing ICS Technologies GmbH. Use of this source code is governed by the AGPL license that can be found in the LICENSE file.

package inspection

import (
	"sync"

	"github.com/Safing/portmaster/network"
	"github.com/Safing/portmaster/network/packet"
)

const (
	DO_NOTHING uint8 = iota
	BLOCK_PACKET
	DROP_PACKET
	BLOCK_LINK
	DROP_LINK
	STOP_INSPECTING
)

type inspectorFn func(packet.Packet, *network.Link) uint8

var (
	inspectors      []inspectorFn
	inspectorNames  []string
	inspectVerdicts []network.Verdict
	inspectorsLock  sync.Mutex
)

func RegisterInspector(name string, inspector inspectorFn, inspectVerdict network.Verdict) (index int) {
	inspectorsLock.Lock()
	defer inspectorsLock.Unlock()
	index = len(inspectors)
	inspectors = append(inspectors, inspector)
	inspectorNames = append(inspectorNames, name)
	inspectVerdicts = append(inspectVerdicts, inspectVerdict)
	return
}

func RunInspectors(pkt packet.Packet, link *network.Link) (network.Verdict, bool) {
	// inspectorsLock.Lock()
	// defer inspectorsLock.Unlock()

	activeInspectors := link.GetActiveInspectors()
	if activeInspectors == nil {
		activeInspectors = make([]bool, len(inspectors), len(inspectors))
		link.SetActiveInspectors(activeInspectors)
	}

	inspectorData := link.GetInspectorData()
	if inspectorData == nil {
		inspectorData = make(map[uint8]interface{})
		link.SetInspectorData(inspectorData)
	}

	continueInspection := false
	verdict := network.VerdictUndecided

	for key, skip := range activeInspectors {

		if skip {
			continue
		}
		if link.Verdict > inspectVerdicts[key] {
			activeInspectors[key] = true
			continue
		}

		action := inspectors[key](pkt, link)
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
		case BLOCK_LINK:
			link.UpdateVerdict(network.VerdictBlock)
			activeInspectors[key] = true
			if verdict < network.VerdictBlock {
				verdict = network.VerdictBlock
			}
		case DROP_LINK:
			link.UpdateVerdict(network.VerdictDrop)
			activeInspectors[key] = true
			verdict = network.VerdictDrop
		case STOP_INSPECTING:
			activeInspectors[key] = true
		}

	}

	return verdict, continueInspection
}
