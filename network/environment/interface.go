// Copyright Safing ICS Technologies GmbH. Use of this source code is governed by the AGPL license that can be found in the LICENSE file.

package environment

import (
	"sync"
	"sync/atomic"
)

type EnvironmentInterface struct {
	lastNetworkChange int64
	lock              sync.Mutex
}

func NewInterface() *EnvironmentInterface {
	return &EnvironmentInterface{
		lastNetworkChange: 0,
	}
}

func (env *EnvironmentInterface) NetworkChanged() bool {
	env.lock.Lock()
	defer env.lock.Unlock()
	lnc := atomic.LoadInt64(lastNetworkChange)
	if lnc > env.lastNetworkChange {
		return true
	}
	return false
}
