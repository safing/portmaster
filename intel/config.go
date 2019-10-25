package intel

import (
	"fmt"
	"strings"

	"github.com/safing/portbase/config"
	"github.com/safing/portmaster/status"
)

var (
	configuredNameServers config.StringArrayOption
	defaultNameServers    = []string{
		// "dot://9.9.9.9:853?verify=dns.quad9.net&",         // Quad9
		// "dot|149.112.112.112:853|dns.quad9.net", // Quad9
		// "dot://[2620:fe::fe]:853?verify=dns.quad9.net&name=Quad9" // Quad9
		// "dot://[2620:fe::9]:853?verify=dns.quad9.net&name=Quad9" // Quad9

		"dot|1.1.1.1:853|cloudflare-dns.com", // Cloudflare
		"dot|1.0.0.1:853|cloudflare-dns.com", // Cloudflare
		"dns|9.9.9.9:53",                     // Quad9
		"dns|149.112.112.112:53",             // Quad9
		"dns|1.1.1.1:53",                     // Cloudflare
		"dns|1.0.0.1:53",                     // Cloudflare
		// "doh|cloudflare-dns.com/dns-query", // DoH still experimental
	}

	nameserverRetryRate         config.IntOption
	doNotUseMulticastDNS        status.SecurityLevelOption
	doNotUseAssignedNameservers status.SecurityLevelOption
	doNotUseInsecureProtocols   status.SecurityLevelOption
	doNotResolveSpecialDomains  status.SecurityLevelOption
	doNotResolveTestDomains     status.SecurityLevelOption
)

func prepConfig() error {
	err := config.Register(&config.Option{
		Name:            "Nameservers (DNS)",
		Key:             "intel/nameservers",
		Description:     "Nameserver to use for resolving DNS requests.",
		OptType:         config.OptTypeStringArray,
		ExpertiseLevel:  config.ExpertiseLevelExpert,
		ReleaseLevel:    config.ReleaseLevelStable,
		DefaultValue:    defaultNameServers,
		ValidationRegex: "^(dns|tcp|tls|https)|[a-z0-9\\.|-]+$",
	})
	if err != nil {
		return err
	}
	configuredNameServers = config.Concurrent.GetAsStringArray("intel/nameservers", defaultNameServers)

	err = config.Register(&config.Option{
		Name:           "Nameserver Retry Rate",
		Key:            "intel/nameserverRetryRate",
		Description:    "Rate at which to retry failed nameservers, in seconds.",
		OptType:        config.OptTypeInt,
		ExpertiseLevel: config.ExpertiseLevelExpert,
		ReleaseLevel:   config.ReleaseLevelStable,
		DefaultValue:   600,
	})
	if err != nil {
		return err
	}
	nameserverRetryRate = config.Concurrent.GetAsInt("intel/nameserverRetryRate", 0)

	err = config.Register(&config.Option{
		Name:            "Do not use Multicast DNS",
		Key:             "intel/doNotUseMulticastDNS",
		Description:     "Multicast DNS queries other devices in the local network",
		OptType:         config.OptTypeInt,
		ExpertiseLevel:  config.ExpertiseLevelExpert,
		ReleaseLevel:    config.ReleaseLevelStable,
		ExternalOptType: "security level",
		DefaultValue:    6,
		ValidationRegex: "^(7|6|4)$",
	})
	if err != nil {
		return err
	}
	doNotUseMulticastDNS = status.ConfigIsActiveConcurrent("intel/doNotUseMulticastDNS")

	err = config.Register(&config.Option{
		Name:            "Do not use assigned Nameservers",
		Key:             "intel/doNotUseAssignedNameservers",
		Description:     "that were acquired by the network (dhcp) or system",
		OptType:         config.OptTypeInt,
		ExpertiseLevel:  config.ExpertiseLevelExpert,
		ReleaseLevel:    config.ReleaseLevelStable,
		ExternalOptType: "security level",
		DefaultValue:    4,
		ValidationRegex: "^(7|6|4)$",
	})
	if err != nil {
		return err
	}
	doNotUseAssignedNameservers = status.ConfigIsActiveConcurrent("intel/doNotUseAssignedNameservers")

	err = config.Register(&config.Option{
		Name:            "Do not resolve insecurely",
		Key:             "intel/doNotUseInsecureProtocols",
		Description:     "Do not resolve domains with insecure protocols, ie. plain DNS",
		OptType:         config.OptTypeInt,
		ExpertiseLevel:  config.ExpertiseLevelExpert,
		ReleaseLevel:    config.ReleaseLevelStable,
		ExternalOptType: "security level",
		DefaultValue:    4,
		ValidationRegex: "^(7|6|4)$",
	})
	if err != nil {
		return err
	}
	doNotUseInsecureProtocols = status.ConfigIsActiveConcurrent("intel/doNotUseInsecureProtocols")

	err = config.Register(&config.Option{
		Name:            "Do not resolve special domains",
		Key:             "intel/doNotResolveSpecialDomains",
		Description:     fmt.Sprintf("Do not resolve the special top level domains %s", formatScopeList(specialServiceScopes)),
		OptType:         config.OptTypeInt,
		ExpertiseLevel:  config.ExpertiseLevelExpert,
		ReleaseLevel:    config.ReleaseLevelStable,
		ExternalOptType: "security level",
		DefaultValue:    7,
		ValidationRegex: "^(7|6|4)$",
	})
	if err != nil {
		return err
	}
	doNotResolveSpecialDomains = status.ConfigIsActiveConcurrent("intel/doNotResolveSpecialDomains")

	err = config.Register(&config.Option{
		Name:            "Do not resolve test domains",
		Key:             "intel/doNotResolveTestDomains",
		Description:     fmt.Sprintf("Do not resolve the special testing top level domains %s", formatScopeList(localTestScopes)),
		OptType:         config.OptTypeInt,
		ExpertiseLevel:  config.ExpertiseLevelExpert,
		ReleaseLevel:    config.ReleaseLevelStable,
		ExternalOptType: "security level",
		DefaultValue:    6,
		ValidationRegex: "^(7|6|4)$",
	})
	if err != nil {
		return err
	}
	doNotResolveTestDomains = status.ConfigIsActiveConcurrent("intel/doNotResolveTestDomains")

	return nil
}

func formatScopeList(list []string) string {
	formatted := make([]string, 0, len(list))
	for _, domain := range list {
		formatted = append(formatted, strings.Trim(domain, "."))
	}
	return strings.Join(formatted, ", ")
}
