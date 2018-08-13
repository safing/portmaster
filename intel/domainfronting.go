// Copyright Safing ICS Technologies GmbH. Use of this source code is governed by the AGPL license that can be found in the LICENSE file.

package intel

import (
	"github.com/Safing/safing-core/log"
	"sync"

	"github.com/miekg/dns"
)

var (
	dfMap     = make(map[string]string)
	dfMapLock sync.RWMutex
)

func checkDomainFronting(hidden string, qtype dns.Type, securityLevel int8) (*RRCache, bool) {
	dfMapLock.RLock()
	front, ok := dfMap[hidden]
	dfMapLock.RUnlock()
	if !ok {
		return nil, false
	}
	log.Tracef("intel: applying domain fronting %s -> %s", hidden, front)
	// get domain name
	rrCache := resolveAndCache(front, qtype, securityLevel)
	if rrCache == nil {
		return nil, true
	}
	// replace domain name
	var header *dns.RR_Header
	for _, rr := range rrCache.Answer {
		header = rr.Header()
		if header.Name == front {
			header.Name = hidden
		}
	}
	// save under front
	rrCache.CreateWithType(hidden, qtype)
	return rrCache, true
}

func addDomainFronting(hidden string, front string) {
	dfMapLock.Lock()
	dfMap[hidden] = front
	dfMapLock.Unlock()
	return
}
