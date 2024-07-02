package resolver

import (
	"errors"
	"fmt"
	"strings"

	"github.com/safing/portmaster/base/config"
	"github.com/safing/portmaster/service/netenv"
	"github.com/safing/portmaster/service/status"
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
		// https://github.com/safing/portmaster/service/wiki/DNS-Server-Settings

		// Quad9 (encrypted DNS)
		// "dot://dns.quad9.net?ip=9.9.9.9&name=Quad9&blockedif=empty",
		// "dot://dns.quad9.net?ip=149.112.112.112&name=Quad9&blockedif=empty",

		// Cloudflare (encrypted DNS, with malware protection)
		"dot://cloudflare-dns.com?ip=1.1.1.2&name=Cloudflare&blockedif=zeroip",
		"dot://cloudflare-dns.com?ip=1.0.0.2&name=Cloudflare&blockedif=zeroip",

		// AdGuard (encrypted DNS, default flavor)
		// "dot://dns.adguard.com?ip=94.140.14.14&name=AdGuard&blockedif=zeroip",
		// "dot://dns.adguard.com?ip=94.140.15.15&name=AdGuard&blockedif=zeroip",

		// Foundation for Applied Privacy (encrypted DNS)
		// "dot://dot1.applied-privacy.net?ip=146.255.56.98&name=AppliedPrivacy",

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
	noAssignedNameservers               config.BoolOption
	cfgOptionNoAssignedNameserversOrder = 1

	CfgOptionUseStaleCacheKey   = "dns/useStaleCache"
	useStaleCacheConfigOption   *config.Option
	useStaleCache               config.BoolOption
	cfgOptionUseStaleCacheOrder = 2

	CfgOptionNoMulticastDNSKey   = "dns/noMulticastDNS"
	noMulticastDNS               config.BoolOption
	cfgOptionNoMulticastDNSOrder = 3

	CfgOptionNoInsecureProtocolsKey   = "dns/noInsecureProtocols"
	noInsecureProtocols               config.BoolOption
	cfgOptionNoInsecureProtocolsOrder = 4

	CfgOptionDontResolveSpecialDomainsKey   = "dns/dontResolveSpecialDomains"
	dontResolveSpecialDomains               config.BoolOption
	cfgOptionDontResolveSpecialDomainsOrder = 16

	CfgOptionNameserverRetryRateKey   = "dns/nameserverRetryRate"
	nameserverRetryRate               config.IntOption
	cfgOptionNameserverRetryRateOrder = 32
)

func prepConfig() error {
	err := config.Register(&config.Option{
		Name:        "DNS Servers",
		Key:         CfgOptionNameServersKey,
		Description: "DNS servers to use for resolving DNS requests.",
		Help: strings.ReplaceAll(`DNS servers are used in the order as entered. The first one will be used as the primary DNS Server. Only if it fails, will the other servers be used as a fallback - in their respective order. If all fail, or if no DNS Server is configured here, the Portmaster will use the one configured in your system or network.

Additionally, if it is more likely that the DNS server of your system or network has a (better) answer to a request, they will be asked first. This will be the case for special local domains and domain spaces announced on the current network.

DNS servers are configured in a URL format. This allows you to specify special settings for a resolver. If you just want to use a resolver at IP 10.2.3.4, please enter: "dns://10.2.3.4"  
The format is: "protocol://host:port?parameter=value&parameter=value"  

For DoH servers, you can also just paste the URL given by the DNS provider.  
When referring to the DNS server using a domain name, as with DoH, it is highly recommended to also specify the IP address using the "ip" parameter, so Portmaster does not have to resolve it.

- Protocol
	- "dot": DNS-over-TLS (or "tls"; recommended)  
	- "doh": DNS-over-HTTPS (or "https")
	- "dns": plain old DNS  
	- "tcp": plain old DNS over TCP
- Host: specify the domain or IP of the resolver
- Port: optionally define a custom port
- Parameters:
	- "name": give your DNS Server a name that is used for messages and logs
	- "verify": domain name to verify for "dot", only valid for "dot" and "doh"
	- "ip": IP address (if using a domain), so Portmaster does not need to resolve it using the system resolver - this is highly recommended
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
					Name:   "Set Cloudflare (with Malware Filter)",
					Action: config.QuickReplace,
					Value: []string{
						"dot://cloudflare-dns.com?ip=1.1.1.2&name=Cloudflare&blockedif=zeroip",
						"dot://cloudflare-dns.com?ip=1.0.0.2&name=Cloudflare&blockedif=zeroip",
					},
				},
				{
					Name:   "Set Quad9",
					Action: config.QuickReplace,
					Value: []string{
						"dot://dns.quad9.net?ip=9.9.9.9&name=Quad9&blockedif=empty",
						"dot://dns.quad9.net?ip=149.112.112.112&name=Quad9&blockedif=empty",
					},
				},
				{
					Name:   "Set AdGuard",
					Action: config.QuickReplace,
					Value: []string{
						"dot://dns.adguard.com?ip=94.140.14.14&name=AdGuard&blockedif=zeroip",
						"dot://dns.adguard.com?ip=94.140.15.15&name=AdGuard&blockedif=zeroip",
					},
				},
				{
					Name:   "Set Foundation for Applied Privacy",
					Action: config.QuickReplace,
					Value: []string{
						"dot://dot1.applied-privacy.net?ip=146.255.56.98&name=AppliedPrivacy",
					},
				},
				{
					Name:   "Add Cloudflare (as fallback)",
					Action: config.QuickMergeBottom,
					Value: []string{
						"dot://cloudflare-dns.com?ip=1.1.1.1&name=Cloudflare&blockedif=zeroip",
						"dot://cloudflare-dns.com?ip=1.0.0.1&name=Cloudflare&blockedif=zeroip",
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
		Name:           "Retry Failing DNS Servers",
		Key:            CfgOptionNameserverRetryRateKey,
		Description:    "Duration in seconds how often failing DNS server should be retried. This is done continuously in the background.",
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
		OptType:        config.OptTypeBool,
		ExpertiseLevel: config.ExpertiseLevelExpert,
		ReleaseLevel:   config.ReleaseLevelStable,
		DefaultValue:   false,
		Annotations: config.Annotations{
			config.DisplayOrderAnnotation:   cfgOptionNoAssignedNameserversOrder,
			config.DisplayHintAnnotation:    status.DisplayHintSecurityLevel,
			config.CategoryAnnotation:       "Servers",
			"self:detail:specialUseDomains": specialUseDomains,
		},
		Migrations: []config.MigrationFunc{status.MigrateSecurityLevelToBoolean},
	})
	if err != nil {
		return err
	}
	noAssignedNameservers = config.Concurrent.GetAsBool(CfgOptionNoAssignedNameserversKey, false)

	useStaleCacheConfigOption = &config.Option{
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
	}
	err = config.Register(useStaleCacheConfigOption)
	if err != nil {
		return err
	}
	useStaleCache = config.Concurrent.GetAsBool(CfgOptionUseStaleCacheKey, false)

	err = config.Register(&config.Option{
		Name:           "Ignore Multicast DNS",
		Key:            CfgOptionNoMulticastDNSKey,
		Description:    "Do not resolve using Multicast DNS. This may break certain Plug and Play devices and services.",
		OptType:        config.OptTypeBool,
		ExpertiseLevel: config.ExpertiseLevelExpert,
		ReleaseLevel:   config.ReleaseLevelStable,
		DefaultValue:   false,
		Annotations: config.Annotations{
			config.DisplayOrderAnnotation:  cfgOptionNoMulticastDNSOrder,
			config.DisplayHintAnnotation:   status.DisplayHintSecurityLevel,
			config.CategoryAnnotation:      "Resolving",
			"self:detail:multicastDomains": multicastDomains,
		},
		Migrations: []config.MigrationFunc{status.MigrateSecurityLevelToBoolean},
	})
	if err != nil {
		return err
	}
	noMulticastDNS = config.Concurrent.GetAsBool(CfgOptionNoMulticastDNSKey, false)

	err = config.Register(&config.Option{
		Name:           "Use Secure Protocols Only",
		Key:            CfgOptionNoInsecureProtocolsKey,
		Description:    "Never resolve using insecure protocols, ie. plain DNS. This may break certain local DNS services, which always use plain DNS.",
		OptType:        config.OptTypeBool,
		ExpertiseLevel: config.ExpertiseLevelExpert,
		ReleaseLevel:   config.ReleaseLevelStable,
		DefaultValue:   false,
		Annotations: config.Annotations{
			config.DisplayOrderAnnotation: cfgOptionNoInsecureProtocolsOrder,
			config.DisplayHintAnnotation:  status.DisplayHintSecurityLevel,
			config.CategoryAnnotation:     "Resolving",
		},
		Migrations: []config.MigrationFunc{status.MigrateSecurityLevelToBoolean},
	})
	if err != nil {
		return err
	}
	noInsecureProtocols = config.Concurrent.GetAsBool(CfgOptionNoInsecureProtocolsKey, false)

	err = config.Register(&config.Option{
		Name: "Block Unofficial TLDs",
		Key:  CfgOptionDontResolveSpecialDomainsKey,
		Description: fmt.Sprintf(
			"Block %s. Unofficial domains may pose a security risk. This setting does not affect .onion domains in the Tor Browser.",
			formatScopeList(specialServiceDomains),
		),
		OptType:        config.OptTypeBool,
		ExpertiseLevel: config.ExpertiseLevelExpert,
		ReleaseLevel:   config.ReleaseLevelStable,
		DefaultValue:   true,
		Annotations: config.Annotations{
			config.DisplayOrderAnnotation:       cfgOptionDontResolveSpecialDomainsOrder,
			config.DisplayHintAnnotation:        status.DisplayHintSecurityLevel,
			config.CategoryAnnotation:           "Resolving",
			"self:detail:specialServiceDomains": specialServiceDomains,
		},
		Migrations: []config.MigrationFunc{status.MigrateSecurityLevelToBoolean},
	})
	if err != nil {
		return err
	}
	dontResolveSpecialDomains = config.Concurrent.GetAsBool(CfgOptionDontResolveSpecialDomainsKey, false)

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
