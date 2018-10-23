package intel

import (
	"github.com/miekg/dns"

	"github.com/Safing/portbase/database"
	"github.com/Safing/portbase/log"
	"github.com/Safing/portbase/modules"
)

func init() {
	modules.Register("intel", prep, start, nil, "database")
}

func start() error {
	_, err := database.Register(&database.Database{
		Name:        "intel",
		Description: "Intelligence and DNS Data",
		StorageType: "badger",
		PrimaryAPI:  "",
	})
	if err != nil {
		return err
	}

	// load resolvers from config and environment
	loadResolvers(false)

	go listenToMDNS()

	return nil
}

func GetIntelAndRRs(domain string, qtype dns.Type, securityLevel uint8) (intel *Intel, rrs *RRCache) {
	intel, err := GetIntel(domain)
	if err != nil {
		log.Errorf("intel: failed to get intel: %s", err)
		intel = nil
	}
	rrs = Resolve(domain, qtype, securityLevel)
	return
}
