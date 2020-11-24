package status

import (
	"sync"

	"github.com/safing/portbase/log"
)

type knownThreats struct {
	sync.RWMutex
	// active threats and their recommended mitigation level
	list map[string]uint8
}

var threats = &knownThreats{
	list: make(map[string]uint8),
}

// SetMitigationLevel sets the mitigation level for id
// to mitigation. If mitigation is SecurityLevelOff the
// mitigation record will be removed. If mitigation is
// an invalid level the call to SetMitigationLevel is a
// no-op.
func SetMitigationLevel(id string, mitigation uint8) {
	if !IsValidSecurityLevel(mitigation) {
		log.Warningf("tried to set invalid mitigation level %d for threat %s", mitigation, id)
		return
	}

	defer triggerAutopilot()

	threats.Lock()
	defer threats.Unlock()
	if mitigation == 0 {
		delete(threats.list, id)
	} else {
		threats.list[id] = mitigation
	}
}

// DeleteMitigationLevel deletes the mitigation level for id.
func DeleteMitigationLevel(id string) {
	SetMitigationLevel(id, SecurityLevelOff)
}

// getHighestMitigationLevel returns the highest mitigation
// level set on a threat.
func getHighestMitigationLevel() uint8 {
	threats.RLock()
	defer threats.RUnlock()

	var level uint8 = SecurityLevelNormal
	for _, lvl := range threats.list {
		if lvl > level {
			level = lvl
		}
	}

	return level
}
