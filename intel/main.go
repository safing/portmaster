package intel

import (
	"github.com/miekg/dns"

	"github.com/Safing/portbase/log"
	"github.com/Safing/portbase/modules"

	// module dependencies
	_ "github.com/Safing/portmaster/core"
)

func init() {
	modules.Register("intel", prep, start, nil, "core")
}

func start() error {
	// load resolvers from config and environment
	loadResolvers(false)

	go listenToMDNS()

	return nil
}

// GetIntelAndRRs returns intel and DNS resource records for the given domain.
func GetIntelAndRRs(domain string, qtype dns.Type, securityLevel uint8) (intel *Intel, rrs *RRCache) {
	intel, err := GetIntel(domain)
	if err != nil {
		log.Errorf("intel: failed to get intel: %s", err)
		intel = nil
	}
	rrs = Resolve(domain, qtype, securityLevel)
	return
}
