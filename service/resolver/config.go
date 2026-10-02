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
		Name:        "DNS 服务器",
		Key:         CfgOptionNameServersKey,
		Description: "用于解析 DNS 请求的 DNS 服务器。",
		Help: strings.ReplaceAll(`DNS 服务器按输入的顺序使用。第一个将作为主 DNS 服务器。只有当它失败时，才会按各自的顺序使用其他服务器作为备用。如果全部失败，或此处未配置任何 DNS 服务器，Portmaster 将使用系统或网络中配置的 DNS 服务器。

此外，如果系统或网络的 DNS 服务器更有可能对某个请求给出（更好的）答复，则会优先询问它们。这适用于特殊的本地域名以及当前网络中公布的域名空间。

DNS 服务器以 URL 格式配置。这使您可以为解析器指定特殊设置。如果您只想使用 IP 为 10.2.3.4 的解析器，请输入："dns://10.2.3.4"  
格式为："protocol://host:port?parameter=value&parameter=value"  

对于 DoH 服务器，您也可以直接粘贴 DNS 提供商给出的 URL。  
当使用域名引用 DNS 服务器时（例如 DoH），强烈建议同时使用 "ip" 参数指定 IP 地址，这样 Portmaster 就无需再解析它。

- 协议（protocol）
	- "dot"：DNS-over-TLS（或 "tls"；推荐）  
	- "doh"：DNS-over-HTTPS（或 "https"）
	- "dns"：传统的明文 DNS  
	- "tcp"：基于 TCP 的传统明文 DNS
- 主机（host）：指定解析器的域名或 IP
- 端口（port）：可选，定义自定义端口
- 参数（parameter）：
	- "name"：为您的 DNS 服务器命名，该名称用于消息和日志
	- "verify"：用于 "dot" 验证的域名，仅对 "dot" 和 "doh" 有效
	- "ip"：IP 地址（如果使用域名），这样 Portmaster 无需通过系统解析器解析它 —— 强烈推荐
	- "blockedif"：检测域名服务器是否阻止了查询，选项：
		- "empty"：服务器以 NXDomain 状态回复，但任何部分中都没有其他记录
		- "refused"：服务器以 Refused 状态回复
		- "zeroip"：服务器回复了 IP 地址，但该地址为零
	- "search"：为此解析器指定优先的域名/顶级域（以 "," 分隔）
	- "search-only"：仅将此解析器用于 "search" 参数中的域名（无需值）
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
			config.CategoryAnnotation:     "服务器",
			config.QuickSettingsAnnotation: []config.QuickSetting{
				{
					Name:   "设为 Cloudflare（含恶意软件过滤）",
					Action: config.QuickReplace,
					Value: []string{
						"dot://cloudflare-dns.com?ip=1.1.1.2&name=Cloudflare&blockedif=zeroip",
						"dot://cloudflare-dns.com?ip=1.0.0.2&name=Cloudflare&blockedif=zeroip",
					},
				},
				{
					Name:   "设为 Quad9",
					Action: config.QuickReplace,
					Value: []string{
						"dot://dns.quad9.net?ip=9.9.9.9&name=Quad9&blockedif=empty",
						"dot://dns.quad9.net?ip=149.112.112.112&name=Quad9&blockedif=empty",
					},
				},
				{
					Name:   "设为 AdGuard",
					Action: config.QuickReplace,
					Value: []string{
						"dot://dns.adguard.com?ip=94.140.14.14&name=AdGuard&blockedif=zeroip",
						"dot://dns.adguard.com?ip=94.140.15.15&name=AdGuard&blockedif=zeroip",
					},
				},
				{
					Name:   "设为 Foundation for Applied Privacy",
					Action: config.QuickReplace,
					Value: []string{
						"dot://dot1.applied-privacy.net?ip=146.255.56.98&name=AppliedPrivacy",
					},
				},
				{
					Name:   "添加 Cloudflare（作为备用）",
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
		Name:           "重试失败的 DNS 服务器",
		Key:            CfgOptionNameserverRetryRateKey,
		Description:    "重试失败 DNS 服务器的间隔时间（秒）。此操作会在后台持续进行。",
		OptType:        config.OptTypeInt,
		ExpertiseLevel: config.ExpertiseLevelDeveloper,
		ReleaseLevel:   config.ReleaseLevelStable,
		DefaultValue:   300,
		Annotations: config.Annotations{
			config.DisplayOrderAnnotation: cfgOptionNameserverRetryRateOrder,
			config.UnitAnnotation:         "秒",
			config.CategoryAnnotation:     "服务器",
		},
		ValidationRegex: `^[1-9][0-9]{1,5}$`,
	})
	if err != nil {
		return err
	}
	nameserverRetryRate = config.Concurrent.GetAsInt(CfgOptionNameserverRetryRateKey, 300)

	err = config.Register(&config.Option{
		Name:           "忽略系统/网络 DNS 服务器",
		Key:            CfgOptionNoAssignedNameserversKey,
		Description:    "忽略系统或网络中配置的 DNS 服务器。这可能导致局域网中的域名无法解析。",
		OptType:        config.OptTypeBool,
		ExpertiseLevel: config.ExpertiseLevelExpert,
		ReleaseLevel:   config.ReleaseLevelStable,
		DefaultValue:   false,
		Annotations: config.Annotations{
			config.DisplayOrderAnnotation:   cfgOptionNoAssignedNameserversOrder,
			config.DisplayHintAnnotation:    status.DisplayHintSecurityLevel,
			config.CategoryAnnotation:       "服务器",
			"self:detail:specialUseDomains": specialUseDomains,
		},
		Migrations: []config.MigrationFunc{status.MigrateSecurityLevelToBoolean},
	})
	if err != nil {
		return err
	}
	noAssignedNameservers = config.Concurrent.GetAsBool(CfgOptionNoAssignedNameserversKey, false)

	useStaleCacheConfigOption = &config.Option{
		Name:           "始终使用 DNS 缓存",
		Key:            CfgOptionUseStaleCacheKey,
		Description:    "始终使用 DNS 缓存，即使条目已过期。过期条目随后会在后台刷新。这可以大幅提升 DNS 解析性能，但可能因 DNS 记录过时而偶尔导致连接错误。",
		OptType:        config.OptTypeBool,
		ExpertiseLevel: config.ExpertiseLevelUser,
		ReleaseLevel:   config.ReleaseLevelStable,
		DefaultValue:   false,
		Annotations: config.Annotations{
			config.DisplayOrderAnnotation: cfgOptionUseStaleCacheOrder,
			config.CategoryAnnotation:     "解析",
		},
	}
	err = config.Register(useStaleCacheConfigOption)
	if err != nil {
		return err
	}
	useStaleCache = config.Concurrent.GetAsBool(CfgOptionUseStaleCacheKey, false)

	err = config.Register(&config.Option{
		Name:           "忽略多播 DNS",
		Key:            CfgOptionNoMulticastDNSKey,
		Description:    "不使用多播 DNS 进行解析。这可能导致某些即插即用设备和服务无法正常工作。",
		OptType:        config.OptTypeBool,
		ExpertiseLevel: config.ExpertiseLevelExpert,
		ReleaseLevel:   config.ReleaseLevelStable,
		DefaultValue:   false,
		Annotations: config.Annotations{
			config.DisplayOrderAnnotation:  cfgOptionNoMulticastDNSOrder,
			config.DisplayHintAnnotation:   status.DisplayHintSecurityLevel,
			config.CategoryAnnotation:      "解析",
			"self:detail:multicastDomains": multicastDomains,
		},
		Migrations: []config.MigrationFunc{status.MigrateSecurityLevelToBoolean},
	})
	if err != nil {
		return err
	}
	noMulticastDNS = config.Concurrent.GetAsBool(CfgOptionNoMulticastDNSKey, false)

	err = config.Register(&config.Option{
		Name:           "仅使用安全协议",
		Key:            CfgOptionNoInsecureProtocolsKey,
		Description:    "绝不使用不安全的协议（即明文 DNS）进行解析。这可能导致某些始终使用明文 DNS 的本地 DNS 服务无法正常工作。",
		OptType:        config.OptTypeBool,
		ExpertiseLevel: config.ExpertiseLevelExpert,
		ReleaseLevel:   config.ReleaseLevelStable,
		DefaultValue:   false,
		Annotations: config.Annotations{
			config.DisplayOrderAnnotation: cfgOptionNoInsecureProtocolsOrder,
			config.DisplayHintAnnotation:  status.DisplayHintSecurityLevel,
			config.CategoryAnnotation:     "解析",
		},
		Migrations: []config.MigrationFunc{status.MigrateSecurityLevelToBoolean},
	})
	if err != nil {
		return err
	}
	noInsecureProtocols = config.Concurrent.GetAsBool(CfgOptionNoInsecureProtocolsKey, false)

	err = config.Register(&config.Option{
		Name: "阻止非官方顶级域",
		Key:  CfgOptionDontResolveSpecialDomainsKey,
		Description: fmt.Sprintf(
			"阻止 %s。非官方域名可能带来安全风险。此设置不影响 Tor 浏览器中的 .onion 域名。",
			formatScopeList(specialServiceDomains),
		),
		OptType:        config.OptTypeBool,
		ExpertiseLevel: config.ExpertiseLevelExpert,
		ReleaseLevel:   config.ReleaseLevelStable,
		DefaultValue:   true,
		Annotations: config.Annotations{
			config.DisplayOrderAnnotation:       cfgOptionDontResolveSpecialDomainsOrder,
			config.DisplayHintAnnotation:        status.DisplayHintSecurityLevel,
			config.CategoryAnnotation:           "解析",
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
