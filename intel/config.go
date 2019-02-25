package intel

import (
	"github.com/Safing/portbase/config"
	"github.com/Safing/portmaster/status"
)

var (
	configuredNameServers config.StringArrayOption
	defaultNameServers    = []string{
		"tls|1.1.1.1:853|cloudflare-dns.com", // Cloudflare
		"tls|1.0.0.1:853|cloudflare-dns.com", // Cloudflare
		"tls|9.9.9.9:853|dns.quad9.net",      // Quad9
		// "https|cloudflare-dns.com/dns-query", // HTTPS still experimental
		"dns|1.1.1.1:53", // Cloudflare
		"dns|1.0.0.1:53", // Cloudflare
		"dns|9.9.9.9:53", // Quad9
	}

	nameserverRetryRate         config.IntOption
	doNotUseMulticastDNS        status.SecurityLevelOption
	doNotUseAssignedNameservers status.SecurityLevelOption
	doNotResolveSpecialDomains  status.SecurityLevelOption
)

func prep() error {
	err := config.Register(&config.Option{
		Name:            "Nameservers (DNS)",
		Key:             "intel/nameservers",
		Description:     "Nameserver to use for resolving DNS requests.",
		ExpertiseLevel:  config.ExpertiseLevelExpert,
		OptType:         config.OptTypeStringArray,
		DefaultValue:    defaultNameServers,
		ValidationRegex: "^(dns|tcp|tls|https)$",
	})
	if err != nil {
		return err
	}
	configuredNameServers = config.Concurrent.GetAsStringArray("intel/nameservers", defaultNameServers)

	err = config.Register(&config.Option{
		Name:           "Nameserver Retry Rate",
		Key:            "intel/nameserverRetryRate",
		Description:    "Rate at which to retry failed nameservers, in seconds.",
		ExpertiseLevel: config.ExpertiseLevelExpert,
		OptType:        config.OptTypeInt,
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
		ExpertiseLevel:  config.ExpertiseLevelExpert,
		OptType:         config.OptTypeInt,
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
		ExpertiseLevel:  config.ExpertiseLevelExpert,
		OptType:         config.OptTypeInt,
		ExternalOptType: "security level",
		DefaultValue:    6,
		ValidationRegex: "^(7|6|4)$",
	})
	if err != nil {
		return err
	}
	doNotUseAssignedNameservers = status.ConfigIsActiveConcurrent("intel/doNotUseAssignedNameservers")

	err = config.Register(&config.Option{
		Name:            "Do not resolve special domains",
		Key:             "intel/doNotResolveSpecialDomains",
		Description:     "Do not resolve special (top level) domains: example, example.com, example.net, example.org, invalid, test, onion. (RFC6761, RFC7686)",
		ExpertiseLevel:  config.ExpertiseLevelExpert,
		OptType:         config.OptTypeInt,
		ExternalOptType: "security level",
		DefaultValue:    6,
		ValidationRegex: "^(7|6|4)$",
	})
	if err != nil {
		return err
	}
	doNotResolveSpecialDomains = status.ConfigIsActiveConcurrent("intel/doNotResolveSpecialDomains")

	return nil
}
