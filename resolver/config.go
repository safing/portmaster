package resolver

import (
	"errors"
	"fmt"
	"strings"

	"github.com/safing/portbase/config"
	"github.com/safing/portmaster/netenv"
	"github.com/safing/portmaster/status"
)

// Configuration Keys.
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
		`dot://1.0.0.2:853?verify=cloudflare-dns.com&name=Cloudflare&blockedif=zeroip`,

		// AdGuard (encrypted DNS, default flavor)
		// `dot://94.140.14.14:853?verify=dns.adguard.com&name=AdGuard&blockedif=zeroip`,
		// `dot://94.140.15.15:853?verify=dns.adguard.com&name=AdGuard&blockedif=zeroip`,

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
		// `dns://94.140.14.14&name=AdGuard&blockedif=zeroip`,
		// `dns://94.140.15.15&name=AdGuard&blockedif=zeroip`,
	}

	CfgOptionNameServersKey   = "dns/nameservers"
	configuredNameServers     config.StringArrayOption
	cfgOptionNameServersOrder = 0

	CfgOptionNoAssignedNameserversKey   = "dns/noAssignedNameservers"
	noAssignedNameservers               status.SecurityLevelOptionFunc
	cfgOptionNoAssignedNameserversOrder = 1

	CfgOptionUseStaleCacheKey   = "dns/useStaleCache"
	useStaleCache               config.BoolOption
	cfgOptionUseStaleCacheOrder = 2

	CfgOptionNoMulticastDNSKey   = "dns/noMulticastDNS"
	noMulticastDNS               status.SecurityLevelOptionFunc
	cfgOptionNoMulticastDNSOrder = 3

	CfgOptionNoInsecureProtocolsKey   = "dns/noInsecureProtocols"
	noInsecureProtocols               status.SecurityLevelOptionFunc
	cfgOptionNoInsecureProtocolsOrder = 4

	CfgOptionDontResolveSpecialDomainsKey   = "dns/dontResolveSpecialDomains"
	dontResolveSpecialDomains               status.SecurityLevelOptionFunc
	cfgOptionDontResolveSpecialDomainsOrder = 16

	CfgOptionNameserverRetryRateKey   = "dns/nameserverRetryRate"
	nameserverRetryRate               config.IntOption
	cfgOptionNameserverRetryRateOrder = 32
)

func prepConfig() error {
	err := config.Register(&config.Option{
		Name:        "DNS Servers",
		Key:         CfgOptionNameServersKey,
		Description: "DNS Servers to use for resolving DNS requests.",
		Help: strings.ReplaceAll(`DNS Servers are used in the order as entered. The first one will be used as the primary DNS Server. Only if it fails, will the other servers be used as a fallback - in their respective order. If all fail, or if no DNS Server is configured here, the Portmaster will use the one configured in your system or network.

Additionally, if it is more likely that the DNS Server of your system or network has a (better) answer to a request, they will be asked first. This will be the case for special local domains and domain spaces announced on the current network.

DNS Servers are configured in a URL format. This allows you to specify special settings for a resolver. If you just want to use a resolver at IP 10.2.3.4, please enter: "dns://10.2.3.4"  
The format is: "protocol://ip:port?parameter=value&parameter=value"  

- Protocol
	- "dot": DNS-over-TLS (recommended)  
	- "dns": plain old DNS  
	- "tcp": plain old DNS over TCP
- IP: always use the IP address and _not_ the domain name!
- Port: optionally define a custom port
- Parameters:
	- "name": give your DNS Server a name that is used for messages and logs
	- "verify": domain name to verify for "dot", required and only valid for protocol "dot"
	- "blockedif": detect if the name server blocks a query, options:
		- "empty": server replies with NXDomain status, but without any other record in any section
		- "refused": server replies with Refused status
		- "zeroip": server replies with an IP address, but it is zero
	- "search": specify prioritized domains/TLDs for this resolver (delimited by ",")
	- "search-only": use this resolver for domains in the "search" parameter only (no value)
`, `"`, "`"),
		Sensitive:       true,
		OptType:         config.OptTypeStringArray,
		ExpertiseLevel:  config.ExpertiseLevelUser,
		ReleaseLevel:    config.ReleaseLevelStable,
		DefaultValue:    defaultNameServers,
		ValidationRegex: fmt.Sprintf("^(%s|%s|%s|%s|%s|%s)://.*", ServerTypeDoT, ServerTypeDoH, ServerTypeDNS, ServerTypeTCP, HTTPSProtocol, TLSProtocol),
		ValidationFunc:  validateNameservers,
		Annotations: config.Annotations{
			config.DisplayHintAnnotation:  config.DisplayHintOrdered,
			config.DisplayOrderAnnotation: cfgOptionNameServersOrder,
			config.CategoryAnnotation:     "Servers",
			config.QuickSettingsAnnotation: []config.QuickSetting{
				{
					Name:   "Cloudflare (with Malware Filter)",
					Action: config.QuickReplace,
					Value: []string{
						"dot://cloudflare-dns.com?ip=1.1.1.2&name=Cloudflare&blockedif=zeroip",
						"dot://cloudflare-dns.com?ip=1.0.0.2&name=Cloudflare&blockedif=zeroip",
					},
				},
				{
					Name:   "Quad9",
					Action: config.QuickReplace,
					Value: []string{
						"dot://dns.quad9.net?ip=9.9.9.9&name=Quad9&blockedif=empty",
						"dot://dns.quad9.net?ip=149.112.112.112&name=Quad9&blockedif=empty",
					},
				},
				{
					Name:   "AdGuard",
					Action: config.QuickReplace,
					Value: []string{
						"dot://dns.adguard.com?ip=94.140.14.14&name=AdGuard&blockedif=zeroip",
						"dot://dns.adguard.com?ip=94.140.15.15&name=AdGuard&blockedif=zeroip",
					},
				},
				{
					Name:   "Foundation for Applied Privacy",
					Action: config.QuickReplace,
					Value: []string{
						"dot://dot1.applied-privacy.net?ip=146.255.56.98&name=AppliedPrivacy",
					},
				},
			},
			"self:detail:internalSpecialUseDomains": internalSpecialUseDomains,
			"self:detail:connectivityDomains":       netenv.ConnectivityDomains,
		},
	})
	if err != nil {
		return err
	}
	configuredNameServers = config.Concurrent.GetAsStringArray(CfgOptionNameServersKey, defaultNameServers)

	err = config.Register(&config.Option{
		Name:           "Ignore Failing DNS Servers Duration",
		Key:            CfgOptionNameserverRetryRateKey,
		Description:    "Duration in seconds how long a failing DNS server should not be retried.",
		OptType:        config.OptTypeInt,
		ExpertiseLevel: config.ExpertiseLevelDeveloper,
		ReleaseLevel:   config.ReleaseLevelStable,
		DefaultValue:   300,
		Annotations: config.Annotations{
			config.DisplayOrderAnnotation: cfgOptionNameserverRetryRateOrder,
			config.UnitAnnotation:         "seconds",
			config.CategoryAnnotation:     "Servers",
		},
		ValidationRegex: `^[1-9][0-9]{1,5}$`,
	})
	if err != nil {
		return err
	}
	nameserverRetryRate = config.Concurrent.GetAsInt(CfgOptionNameserverRetryRateKey, 300)

	err = config.Register(&config.Option{
		Name:           "Ignore System/Network Servers",
		Key:            CfgOptionNoAssignedNameserversKey,
		Description:    "Ignore DNS servers configured in your system or network. This may break domains from your local network.",
		OptType:        config.OptTypeInt,
		ExpertiseLevel: config.ExpertiseLevelExpert,
		ReleaseLevel:   config.ReleaseLevelStable,
		DefaultValue:   status.SecurityLevelsHighAndExtreme,
		PossibleValues: status.SecurityLevelValues,
		Annotations: config.Annotations{
			config.DisplayOrderAnnotation:   cfgOptionNoAssignedNameserversOrder,
			config.DisplayHintAnnotation:    status.DisplayHintSecurityLevel,
			config.CategoryAnnotation:       "Servers",
			"self:detail:specialUseDomains": specialUseDomains,
		},
	})
	if err != nil {
		return err
	}
	noAssignedNameservers = status.SecurityLevelOption(CfgOptionNoAssignedNameserversKey)

	err = config.Register(&config.Option{
		Name:           "Always Use DNS Cache",
		Key:            CfgOptionUseStaleCacheKey,
		Description:    "Always use the DNS cache, even if entries have expired. Expired entries are refreshed afterwards in the background. This can improve DNS resolving performance a lot, but may lead to occasional connection errors due to outdated DNS records.",
		OptType:        config.OptTypeBool,
		ExpertiseLevel: config.ExpertiseLevelUser,
		ReleaseLevel:   config.ReleaseLevelStable,
		DefaultValue:   false,
		Annotations: config.Annotations{
			config.DisplayOrderAnnotation: cfgOptionUseStaleCacheOrder,
			config.CategoryAnnotation:     "Resolving",
		},
	})
	if err != nil {
		return err
	}
	useStaleCache = config.Concurrent.GetAsBool(CfgOptionUseStaleCacheKey, false)

	err = config.Register(&config.Option{
		Name:           "Ignore Multicast DNS",
		Key:            CfgOptionNoMulticastDNSKey,
		Description:    "Do not resolve using Multicast DNS. This may break certain Plug and Play devices and services.",
		OptType:        config.OptTypeInt,
		ExpertiseLevel: config.ExpertiseLevelExpert,
		ReleaseLevel:   config.ReleaseLevelStable,
		DefaultValue:   status.SecurityLevelsHighAndExtreme,
		PossibleValues: status.SecurityLevelValues,
		Annotations: config.Annotations{
			config.DisplayOrderAnnotation:  cfgOptionNoMulticastDNSOrder,
			config.DisplayHintAnnotation:   status.DisplayHintSecurityLevel,
			config.CategoryAnnotation:      "Resolving",
			"self:detail:multicastDomains": multicastDomains,
		},
	})
	if err != nil {
		return err
	}
	noMulticastDNS = status.SecurityLevelOption(CfgOptionNoMulticastDNSKey)

	err = config.Register(&config.Option{
		Name:           "Use Secure Protocols Only",
		Key:            CfgOptionNoInsecureProtocolsKey,
		Description:    "Never resolve using insecure protocols, ie. plain DNS. This may break certain local DNS services, which always use plain DNS.",
		OptType:        config.OptTypeInt,
		ExpertiseLevel: config.ExpertiseLevelExpert,
		ReleaseLevel:   config.ReleaseLevelStable,
		DefaultValue:   status.SecurityLevelsHighAndExtreme,
		PossibleValues: status.SecurityLevelValues,
		Annotations: config.Annotations{
			config.DisplayOrderAnnotation: cfgOptionNoInsecureProtocolsOrder,
			config.DisplayHintAnnotation:  status.DisplayHintSecurityLevel,
			config.CategoryAnnotation:     "Resolving",
		},
	})
	if err != nil {
		return err
	}
	noInsecureProtocols = status.SecurityLevelOption(CfgOptionNoInsecureProtocolsKey)

	err = config.Register(&config.Option{
		Name: "Block Unofficial TLDs",
		Key:  CfgOptionDontResolveSpecialDomainsKey,
		Description: fmt.Sprintf(
			"Block %s. Unofficial domains may pose a security risk. This setting does not affect .onion domains in the Tor Browser.",
			formatScopeList(specialServiceDomains),
		),
		OptType:        config.OptTypeInt,
		ExpertiseLevel: config.ExpertiseLevelExpert,
		ReleaseLevel:   config.ReleaseLevelStable,
		DefaultValue:   status.SecurityLevelsAll,
		PossibleValues: status.AllSecurityLevelValues,
		Annotations: config.Annotations{
			config.DisplayOrderAnnotation:       cfgOptionDontResolveSpecialDomainsOrder,
			config.DisplayHintAnnotation:        status.DisplayHintSecurityLevel,
			config.CategoryAnnotation:           "Resolving",
			"self:detail:specialServiceDomains": specialServiceDomains,
		},
	})
	if err != nil {
		return err
	}
	dontResolveSpecialDomains = status.SecurityLevelOption(CfgOptionDontResolveSpecialDomainsKey)

	return nil
}

func validateNameservers(value interface{}) error {
	list, ok := value.([]string)
	if !ok {
		return errors.New("invalid type")
	}

	for i, entry := range list {
		_, _, err := createResolver(entry, ServerSourceConfigured)
		if err != nil {
			return fmt.Errorf("failed to parse DNS server \"%s\" (#%d): %w", entry, i+1, err)
		}
	}

	return nil
}

func formatScopeList(list []string) string {
	formatted := make([]string, 0, len(list))
	for _, domain := range list {
		formatted = append(formatted, strings.TrimRight(domain, "."))
	}
	return strings.Join(formatted, ", ")
}
