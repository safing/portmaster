package resolver

import (
	"fmt"
	"strings"

	"github.com/safing/portbase/config"
	"github.com/safing/portmaster/status"
)

// Configuration Keys
var (
	defaultNameServers = []string{
		// Collection of default DNS Servers

		// Default servers should be:
		// Anycast:
		// - Servers should be reachable from anywhere with reasonable latency.
		// - Servers should be near to the user for geo-content to work correctly.
		// Private:
		// - Servers should not do any or only minimal logging.
		// - Available logging data may not be used against the user, ie. unethically.

		// Sadly, only a few services come close to fulfilling these requirements.
		// For now, we have settled for two bigger and well known services: Cloudflare and Quad9.
		// TODO: monitor situation and re-evaluate when new services become available
		// TODO: explore other methods of making queries more private

		// We encourage everyone who has the technical abilities to set their own preferred servers.

		// Default 1: Cloudflare
		"dot://1.1.1.1:853?verify=cloudflare-dns.com&name=Cloudflare&blockedif=zeroip", // Cloudflare
		"dot://1.0.0.1:853?verify=cloudflare-dns.com&name=Cloudflare&blockedif=zeroip", // Cloudflare

		// Default 2: Quad9
		"dot://9.9.9.9:853?verify=dns.quad9.net&name=Quad9&blockedif=empty",         // Quad9
		"dot://149.112.112.112:853?verify=dns.quad9.net&name=Quad9&blockedif=empty", // Quad9

		// Fallback 1: Cloudflare
		"dns://1.1.1.1:53?name=Cloudflare&blockedif=zeroip", // Cloudflare
		"dns://1.0.0.1:53?name=Cloudflare&blockedif=zeroip", // Cloudflare

		// Fallback 2: Quad9
		"dns://9.9.9.9:53?name=Quad9&blockedif=empty",         // Quad9
		"dns://149.112.112.112:53?name=Quad9&blockedif=empty", // Quad9

		// supported parameters
		// - `verify=domain`: verify domain (dot only)
		// future parameters:
		//
		// - `name=name`: human readable name for resolver
		// - `blockedif=empty`: how to detect if the dns service blocked something
		//	- `empty`: NXDomain result, but without any other record in any section
		//  - `refused`: Request was refused
		//	- `zeroip`: Answer only contains zeroip
	}

	CfgOptionNameServersKey = "dns/nameservers"
	configuredNameServers   config.StringArrayOption

	CfgOptionNameserverRetryRateKey = "dns/nameserverRetryRate"
	nameserverRetryRate             config.IntOption

	CfgOptionNoMulticastDNSKey = "dns/noMulticastDNS"
	noMulticastDNS             status.SecurityLevelOption

	CfgOptionNoAssignedNameserversKey = "dns/noAssignedNameservers"
	noAssignedNameservers             status.SecurityLevelOption

	CfgOptionNoInsecureProtocolsKey = "dns/noInsecureProtocols"
	noInsecureProtocols             status.SecurityLevelOption

	CfgOptionDontResolveSpecialDomainsKey = "dns/dontResolveSpecialDomains"
	dontResolveSpecialDomains             status.SecurityLevelOption

	CfgOptionDontResolveTestDomainsKey = "dns/dontResolveTestDomains"
	dontResolveTestDomains             status.SecurityLevelOption
)

func prepConfig() error {
	err := config.Register(&config.Option{
		Name:            "DNS Servers",
		Key:             CfgOptionNameServersKey,
		Description:     "DNS Servers to use for resolving DNS requests.",
		OptType:         config.OptTypeStringArray,
		ExpertiseLevel:  config.ExpertiseLevelExpert,
		ReleaseLevel:    config.ReleaseLevelStable,
		DefaultValue:    defaultNameServers,
		ValidationRegex: "^(dns|dot|tls)://.*",
	})
	if err != nil {
		return err
	}
	configuredNameServers = config.Concurrent.GetAsStringArray(CfgOptionNameServersKey, defaultNameServers)

	err = config.Register(&config.Option{
		Name:           "DNS Server Retry Rate",
		Key:            CfgOptionNameserverRetryRateKey,
		Description:    "Rate at which to retry failed DNS Servers, in seconds.",
		OptType:        config.OptTypeInt,
		ExpertiseLevel: config.ExpertiseLevelExpert,
		ReleaseLevel:   config.ReleaseLevelStable,
		DefaultValue:   600,
	})
	if err != nil {
		return err
	}
	nameserverRetryRate = config.Concurrent.GetAsInt(CfgOptionNameserverRetryRateKey, 600)

	err = config.Register(&config.Option{
		Name:            "Do not use Multicast DNS",
		Key:             CfgOptionNoMulticastDNSKey,
		Description:     "Multicast DNS queries other devices in the local network",
		OptType:         config.OptTypeInt,
		ExpertiseLevel:  config.ExpertiseLevelExpert,
		ReleaseLevel:    config.ReleaseLevelStable,
		ExternalOptType: "security level",
		DefaultValue:    status.SecurityLevelsHighAndExtreme,
		ValidationRegex: "^(4|6|7)$",
	})
	if err != nil {
		return err
	}
	noMulticastDNS = status.ConfigIsActiveConcurrent(CfgOptionNoMulticastDNSKey)

	err = config.Register(&config.Option{
		Name:            "Do not use assigned Nameservers",
		Key:             CfgOptionNoAssignedNameserversKey,
		Description:     "that were acquired by the network (dhcp) or system",
		OptType:         config.OptTypeInt,
		ExpertiseLevel:  config.ExpertiseLevelExpert,
		ReleaseLevel:    config.ReleaseLevelStable,
		ExternalOptType: "security level",
		DefaultValue:    status.SecurityLevelsHighAndExtreme,
		ValidationRegex: "^(4|6|7)$",
	})
	if err != nil {
		return err
	}
	noAssignedNameservers = status.ConfigIsActiveConcurrent(CfgOptionNoAssignedNameserversKey)

	err = config.Register(&config.Option{
		Name:            "Do not resolve insecurely",
		Key:             CfgOptionNoInsecureProtocolsKey,
		Description:     "Do not resolve domains with insecure protocols, ie. plain DNS",
		OptType:         config.OptTypeInt,
		ExpertiseLevel:  config.ExpertiseLevelExpert,
		ReleaseLevel:    config.ReleaseLevelStable,
		ExternalOptType: "security level",
		DefaultValue:    status.SecurityLevelsHighAndExtreme,
		ValidationRegex: "^(4|6|7)$",
	})
	if err != nil {
		return err
	}
	noInsecureProtocols = status.ConfigIsActiveConcurrent(CfgOptionNoInsecureProtocolsKey)

	err = config.Register(&config.Option{
		Name:            "Do not resolve special domains",
		Key:             CfgOptionDontResolveSpecialDomainsKey,
		Description:     fmt.Sprintf("Do not resolve the special top level domains %s", formatScopeList(specialServiceScopes)),
		OptType:         config.OptTypeInt,
		ExpertiseLevel:  config.ExpertiseLevelExpert,
		ReleaseLevel:    config.ReleaseLevelStable,
		ExternalOptType: "security level",
		DefaultValue:    status.SecurityLevelsAll,
		ValidationRegex: "^(4|6|7)$",
	})
	if err != nil {
		return err
	}
	dontResolveSpecialDomains = status.ConfigIsActiveConcurrent(CfgOptionDontResolveSpecialDomainsKey)

	err = config.Register(&config.Option{
		Name:            "Do not resolve test domains",
		Key:             CfgOptionDontResolveTestDomainsKey,
		Description:     fmt.Sprintf("Do not resolve the special testing top level domains %s", formatScopeList(localTestScopes)),
		OptType:         config.OptTypeInt,
		ExpertiseLevel:  config.ExpertiseLevelExpert,
		ReleaseLevel:    config.ReleaseLevelStable,
		ExternalOptType: "security level",
		DefaultValue:    status.SecurityLevelsHighAndExtreme,
		ValidationRegex: "^(4|6|7)$",
	})
	if err != nil {
		return err
	}
	dontResolveTestDomains = status.ConfigIsActiveConcurrent(CfgOptionDontResolveTestDomainsKey)

	return nil
}

func formatScopeList(list []string) string {
	formatted := make([]string, 0, len(list))
	for _, domain := range list {
		formatted = append(formatted, strings.Trim(domain, "."))
	}
	return strings.Join(formatted, ", ")
}
