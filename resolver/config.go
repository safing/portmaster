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

		// For a detailed explanation how we choose our default resolvers, check out
		// https://safing.io/blog/2020/07/07/how-safing-selects-its-default-dns-providers/

		// These resolvers define a working set. Which provider we selected as the
		// primary depends on the current situation.

		// We encourage everyone who has the technical abilities to set their own preferred servers.
		// For a list of configuration options, see
		// https://github.com/safing/portmaster/wiki/DNS-Server-Settings

		// Quad9 (encrypted DNS)
		// `dot://9.9.9.9:853?verify=dns.quad9.net&name=Quad9&blockedif=empty`,
		// `dot://149.112.112.112:853?verify=dns.quad9.net&name=Quad9&blockedif=empty`,

		// Cloudflare (encrypted DNS, with malware protection)
		`dot://1.1.1.2:853?verify=cloudflare-dns.com&name=Cloudflare&blockedif=zeroip`,
		// `dot://1.0.0.2:853?verify=cloudflare-dns.com&name=Cloudflare&blockedif=zeroip`,

		// AdGuard (encrypted DNS, default flavor)
		// `dot://176.103.130.130:853?verify=dns.adguard.com&name=AdGuard&blockedif=zeroip`,
		// `dot://176.103.130.131:853?verify=dns.adguard.com&name=AdGuard&blockedif=zeroip`,

		// Foundation for Applied Privacy (encrypted DNS)
		// `dot://94.130.106.88:853?verify=dot1.applied-privacy.net&name=AppliedPrivacy`,
		// `dot://94.130.106.88:443?verify=dot1.applied-privacy.net&name=AppliedPrivacy`,

		// Quad9 (plain DNS)
		// `dns://9.9.9.9:53?name=Quad9&blockedif=empty`,
		// `dns://149.112.112.112:53?name=Quad9&blockedif=empty`,

		// Cloudflare (plain DNS, with malware protection)
		// `dns://1.1.1.2:53?name=Cloudflare&blockedif=zeroip`,
		// `dns://1.0.0.2:53?name=Cloudflare&blockedif=zeroip`,

		// AdGuard (plain DNS, default flavor)
		// `dns://176.103.130.130&name=AdGuard&blockedif=zeroip`,
		// `dns://176.103.130.131&name=AdGuard&blockedif=zeroip`,
	}

	CfgOptionNameServersKey   = "dns/nameservers"
	configuredNameServers     config.StringArrayOption
	cfgOptionNameServersOrder = 0

	CfgOptionNoAssignedNameserversKey   = "dns/noAssignedNameservers"
	noAssignedNameservers               status.SecurityLevelOption
	cfgOptionNoAssignedNameserversOrder = 1

	CfgOptionNoMulticastDNSKey   = "dns/noMulticastDNS"
	noMulticastDNS               status.SecurityLevelOption
	cfgOptionNoMulticastDNSOrder = 2

	CfgOptionNoInsecureProtocolsKey   = "dns/noInsecureProtocols"
	noInsecureProtocols               status.SecurityLevelOption
	cfgOptionNoInsecureProtocolsOrder = 3

	CfgOptionDontResolveSpecialDomainsKey   = "dns/dontResolveSpecialDomains"
	dontResolveSpecialDomains               status.SecurityLevelOption
	cfgOptionDontResolveSpecialDomainsOrder = 16

	CfgOptionDontResolveTestDomainsKey   = "dns/dontResolveTestDomains"
	dontResolveTestDomains               status.SecurityLevelOption
	cfgOptionDontResolveTestDomainsOrder = 17

	CfgOptionNameserverRetryRateKey   = "dns/nameserverRetryRate"
	nameserverRetryRate               config.IntOption
	cfgOptionNameserverRetryRateOrder = 32
)

func prepConfig() error {
	err := config.Register(&config.Option{
		Name:        "DNS Servers",
		Key:         CfgOptionNameServersKey,
		Description: "DNS Servers to use for resolving DNS requests.",
		Help: `Format:

DNS Servers are configured in a URL format. This allows you to specify special settings for a resolver. If you just want to use a resolver at IP 10.2.3.4, please enter: dns://10.2.3.4:53
The format is: protocol://ip:port?parameter=value&parameter=value

Protocols:
	dot: DNS-over-TLS (recommended)
	dns: plain old DNS
	tcp: plain old DNS over TCP

IP:
	always use the IP address and _not_ the domain name!

Port:
	always add the port!

Parameters:
	name: give your DNS Server a name that is used for messages and logs
	verify: domain name to verify for "dot", required and only valid for "dot"
	blockedif: detect if the name server blocks a query, options:
		empty: server replies with NXDomain status, but without any other record in any section
		refused: server replies with Refused status
		zeroip: server replies with an IP address, but it is zero
`,
		Order:           cfgOptionNameServersOrder,
		OptType:         config.OptTypeStringArray,
		ExpertiseLevel:  config.ExpertiseLevelExpert,
		ReleaseLevel:    config.ReleaseLevelStable,
		DefaultValue:    defaultNameServers,
		ValidationRegex: fmt.Sprintf("^(%s|%s|%s)://.*", ServerTypeDoT, ServerTypeDNS, ServerTypeTCP),
	})
	if err != nil {
		return err
	}
	configuredNameServers = config.Concurrent.GetAsStringArray(CfgOptionNameServersKey, defaultNameServers)

	err = config.Register(&config.Option{
		Name:           "DNS Server Retry Rate",
		Key:            CfgOptionNameserverRetryRateKey,
		Description:    "Rate at which to retry failed DNS Servers, in seconds.",
		Order:          cfgOptionNameserverRetryRateOrder,
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
		Order:           cfgOptionNoMulticastDNSOrder,
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
		Order:           cfgOptionNoAssignedNameserversOrder,
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
		Order:           cfgOptionNoInsecureProtocolsOrder,
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
		Order:           cfgOptionDontResolveSpecialDomainsOrder,
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
		Order:           cfgOptionDontResolveTestDomainsOrder,
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
