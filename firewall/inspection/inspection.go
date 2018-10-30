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

	if link.ActiveInspectors == nil {
		link.ActiveInspectors = make([]bool, len(inspectors), len(inspectors))
	}

	if link.InspectorData == nil {
		link.InspectorData = make(map[uint8]interface{})
	}

	continueInspection := false
	verdict := network.UNDECIDED

	for key, skip := range link.ActiveInspectors {

		if skip {
			continue
		}
		if link.Verdict > inspectVerdicts[key] {
			link.ActiveInspectors[key] = true
			continue
		}

		action := inspectors[key](pkt, link)
		switch action {
		case DO_NOTHING:
			if verdict < network.ACCEPT {
				verdict = network.ACCEPT
			}
			continueInspection = true
		case BLOCK_PACKET:
			if verdict < network.BLOCK {
				verdict = network.BLOCK
			}
			continueInspection = true
		case DROP_PACKET:
			verdict = network.DROP
			continueInspection = true
		case BLOCK_LINK:
			link.UpdateVerdict(network.BLOCK)
			link.ActiveInspectors[key] = true
			if verdict < network.BLOCK {
				verdict = network.BLOCK
			}
		case DROP_LINK:
			link.UpdateVerdict(network.DROP)
			link.ActiveInspectors[key] = true
			verdict = network.DROP
		case STOP_INSPECTING:
			link.ActiveInspectors[key] = true
		}

	}

	return verdict, continueInspection
}
