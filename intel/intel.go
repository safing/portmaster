// Copyright Safing ICS Technologies GmbH. Use of this source code is governed by the AGPL license that can be found in the LICENSE file.

package intel

import (
	"github.com/Safing/safing-core/database"
	"github.com/Safing/safing-core/modules"

	"github.com/miekg/dns"
)

var (
	intelModule *modules.Module
)

func init() {
	intelModule = modules.Register("Intel", 128)
	go Start()
}

// GetIntel returns an Intel object of the given domain. The returned Intel object MUST not be modified.
func GetIntel(domain string) *Intel {
	fqdn := dns.Fqdn(domain)
	intel, err := getIntel(fqdn)
	if err != nil {
		if err == database.ErrNotFound {
			intel = &Intel{Domain: fqdn}
			intel.Create(fqdn)
		} else {
			return nil
		}
	}
	return intel
}

func GetIntelAndRRs(domain string, qtype dns.Type, securityLevel int8) (intel *Intel, rrs *RRCache) {
	intel = GetIntel(domain)
	rrs = Resolve(domain, qtype, securityLevel)
	return
}

func Start() {
	// mocking until intel has its own goroutines
	defer intelModule.StopComplete()
	<-intelModule.Stop
}
