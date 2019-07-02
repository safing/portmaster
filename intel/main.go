package intel

import (
	"context"

	"github.com/miekg/dns"

	"github.com/safing/portbase/log"
	"github.com/safing/portbase/modules"

	// module dependencies
	_ "github.com/safing/portmaster/core"
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
func GetIntelAndRRs(ctx context.Context, domain string, qtype dns.Type, securityLevel uint8) (intel *Intel, rrs *RRCache) {
	log.Tracer(ctx).Trace("intel: getting intel")
	intel, err := GetIntel(domain)
	if err != nil {
		log.Tracer(ctx).Warningf("intel: failed to get intel: %s", err)
		log.Errorf("intel: failed to get intel: %s", err)
		intel = nil
	}

	log.Tracer(ctx).Tracef("intel: getting records")
	rrs = Resolve(ctx, domain, qtype, securityLevel)
	return
}
