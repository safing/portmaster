package profile

import (
	"errors"
	"strings"

	"github.com/safing/portmaster/base/config"
	"github.com/safing/portmaster/service/profile/endpoints"
	"github.com/safing/portmaster/service/status"
	"github.com/safing/portmaster/spn/access/account"
)

// Configuration Keys.
var (
	cfgStringOptions      = make(map[string]config.StringOption)
	cfgStringArrayOptions = make(map[string]config.StringArrayOption)
	cfgIntOptions         = make(map[string]config.IntOption)
	cfgBoolOptions        = make(map[string]config.BoolOption)

	// General.

	// Setting "Enable Filter" at order 0.

	CfgOptionDefaultActionKey   = "filter/defaultAction"
	cfgOptionDefaultAction      config.StringOption
	cfgOptionDefaultActionOrder = 1

	DefaultActionPermitValue = "permit"
	DefaultActionBlockValue  = "block"
	DefaultActionAskValue    = "ask"

	// Setting "Prompt Desktop Notifications" at order 2.
	// Setting "Prompt Timeout" at order 3.

	// Network Scopes.

	CfgOptionBlockScopeInternetKey   = "filter/blockInternet"
	cfgOptionBlockScopeInternet      config.BoolOption
	cfgOptionBlockScopeInternetOrder = 16

	CfgOptionBlockScopeLANKey   = "filter/blockLAN"
	cfgOptionBlockScopeLAN      config.BoolOption
	cfgOptionBlockScopeLANOrder = 17

	CfgOptionBlockScopeLocalKey   = "filter/blockLocal"
	cfgOptionBlockScopeLocal      config.BoolOption
	cfgOptionBlockScopeLocalOrder = 18

	// Connection Types.

	CfgOptionBlockP2PKey   = "filter/blockP2P"
	cfgOptionBlockP2P      config.BoolOption
	cfgOptionBlockP2POrder = 19

	CfgOptionBlockInboundKey   = "filter/blockInbound"
	cfgOptionBlockInbound      config.BoolOption
	cfgOptionBlockInboundOrder = 20

	// Rules.

	CfgOptionEndpointsKey   = "filter/endpoints"
	cfgOptionEndpoints      config.StringArrayOption
	cfgOptionEndpointsOrder = 32

	CfgOptionServiceEndpointsKey   = "filter/serviceEndpoints"
	cfgOptionServiceEndpoints      config.StringArrayOption
	cfgOptionServiceEndpointsOrder = 33

	CfgOptionFilterListsKey   = "filter/lists"
	cfgOptionFilterLists      config.StringArrayOption
	cfgOptionFilterListsOrder = 34

	// Setting "Custom Filter List" at order 35.

	CfgOptionFilterSubDomainsKey   = "filter/includeSubdomains"
	cfgOptionFilterSubDomains      config.BoolOption
	cfgOptionFilterSubDomainsOrder = 36

	// DNS Filtering.

	CfgOptionFilterCNAMEKey   = "filter/includeCNAMEs"
	cfgOptionFilterCNAME      config.BoolOption
	cfgOptionFilterCNAMEOrder = 48

	CfgOptionRemoveOutOfScopeDNSKey   = "filter/removeOutOfScopeDNS"
	cfgOptionRemoveOutOfScopeDNS      config.BoolOption
	cfgOptionRemoveOutOfScopeDNSOrder = 49

	CfgOptionRemoveBlockedDNSKey   = "filter/removeBlockedDNS"
	cfgOptionRemoveBlockedDNS      config.BoolOption
	cfgOptionRemoveBlockedDNSOrder = 50

	CfgOptionDomainHeuristicsKey   = "filter/domainHeuristics"
	cfgOptionDomainHeuristics      config.BoolOption
	cfgOptionDomainHeuristicsOrder = 51

	// Advanced.

	CfgOptionPreventBypassingKey   = "filter/preventBypassing"
	cfgOptionPreventBypassing      config.BoolOption
	cfgOptionPreventBypassingOrder = 64

	CfgOptionDisableAutoPermitKey   = "filter/disableAutoPermit"
	cfgOptionDisableAutoPermit      config.BoolOption
	cfgOptionDisableAutoPermitOrder = 65

	// Setting "Permanent Verdicts" at order 80.

	// Network History.

	CfgOptionEnableHistoryKey   = "history/enable"
	cfgOptionEnableHistory      config.BoolOption
	cfgOptionEnableHistoryOrder = 96

	CfgOptionKeepHistoryKey   = "history/keep"
	cfgOptionKeepHistory      config.IntOption
	cfgOptionKeepHistoryOrder = 97

	// Setting "Enable SPN" at order 128.

	CfgOptionUseSPNKey   = "spn/use"
	cfgOptionUseSPN      config.BoolOption
	cfgOptionUseSPNOrder = 129

	CfgOptionSPNUsagePolicyKey   = "spn/usagePolicy"
	cfgOptionSPNUsagePolicy      config.StringArrayOption
	cfgOptionSPNUsagePolicyOrder = 130

	CfgOptionRoutingAlgorithmKey   = "spn/routingAlgorithm"
	cfgOptionRoutingAlgorithm      config.StringOption
	cfgOptionRoutingAlgorithmOrder = 144
	DefaultRoutingProfileID        = "double-hop" // Copied due to import loop.

	// Setting "Home Node Rules" at order 145.

	CfgOptionTransitHubPolicyKey   = "spn/transitHubPolicy"
	cfgOptionTransitHubPolicy      config.StringArrayOption
	cfgOptionTransitHubPolicyOrder = 146

	CfgOptionExitHubPolicyKey   = "spn/exitHubPolicy"
	cfgOptionExitHubPolicy      config.StringArrayOption
	cfgOptionExitHubPolicyOrder = 147

	// Setting "DNS Exit Node Rules" at order 148.

	// Split Tunnel.
	CfgOptionSplitTunUseKey   = "splittun/use"
	cfgOptionSplitTunUse      config.BoolOption
	cfgOptionSplitTunUseOrder = 212

	CfgOptionSplitTunInterfaceKey   = "splittun/networkInterface"
	cfgOptionSplitTunInterface      config.StringOption
	cfgOptionSplitTunInterfaceOrder = 214

	CfgOptionSplitTunUsagePolicyKey   = "splittun/usagePolicy"
	cfgOptionSplitTunUsagePolicy      config.StringArrayOption
	cfgOptionSplitTunUsagePolicyOrder = 216
)

var (
	// SPNRulesQuickSettings are now generated automatically shorty after start.
	SPNRulesQuickSettings = []config.QuickSetting{
		{Name: "加载中...", Action: config.QuickMergeTop, Value: []string{""}},
	}

	// SPNRulesVerdictNames defines the verdicts names to be used for SPN Rules.
	SPNRulesVerdictNames = map[string]string{
		"-": "排除", // Default.
		"+": "允许",
	}

	// SPNRulesHelp defines the help text for SPN related Hub selection rules.
	SPNRulesHelp = strings.ReplaceAll(`规则按从上到下的顺序检查，在第一个匹配项处停止。规则可以匹配 SPN 节点的以下属性：

- 国家/地区（基于 IP）："US"（依据 ISO 3166-1 alpha-2 的两字母国家/地区代码）
- AS 编号："AS123456"
- 地址："192.168.0.1"
- 网络："192.168.0.1/24"
- 任意："*"
`, `"`, "`")
)

func registerConfiguration() error { //nolint:maintidx
	// Default Filter Action
	// permit - blocklist mode: everything is allowed unless blocked
	// ask - ask mode: if not verdict is found, the user is consulted
	// block - allowlist mode: everything is blocked unless explicitly allowed
	err := config.Register(&config.Option{
		Name:         "默认网络操作",
		Key:          CfgOptionDefaultActionKey,
		Description:  `当没有其他设置允许或阻止某个连接时，将应用默认网络操作。此设置同时影响出站和入站连接。它是所有设置中优先级最低的，通常会被“强制阻止”类设置或规则覆盖。`,
		OptType:      config.OptTypeString,
		DefaultValue: DefaultActionPermitValue,
		Annotations: config.Annotations{
			config.SettablePerAppAnnotation: true,
			config.DisplayHintAnnotation:    config.DisplayHintOneOf,
			config.DisplayOrderAnnotation:   cfgOptionDefaultActionOrder,
			config.CategoryAnnotation:       "常规",
		},
		PossibleValues: []config.PossibleValue{
			{
				Name:        "允许",
				Value:       DefaultActionPermitValue,
				Description: "允许所有连接",
			},
			{
				Name:        "阻止",
				Value:       DefaultActionBlockValue,
				Description: "阻止所有连接",
			},
			{
				Name:        "询问",
				Value:       DefaultActionAskValue,
				Description: "询问由您决定",
			},
		},
	})
	if err != nil {
		return err
	}
	cfgOptionDefaultAction = config.Concurrent.GetAsString(CfgOptionDefaultActionKey, DefaultActionPermitValue)
	cfgStringOptions[CfgOptionDefaultActionKey] = cfgOptionDefaultAction

	// Disable Auto Permit
	err = config.Register(&config.Option{
		// TODO: Check how to best handle negation here.
		Name:         "禁用自动允许",
		Key:          CfgOptionDisableAutoPermitKey,
		Description:  `自动允许会查找应用与连接目标之间的关联——如果存在关联，该连接将被允许。`,
		OptType:      config.OptTypeBool,
		ReleaseLevel: config.ReleaseLevelBeta,
		DefaultValue: true,
		Annotations: config.Annotations{
			config.SettablePerAppAnnotation: true,
			config.DisplayOrderAnnotation:   cfgOptionDisableAutoPermitOrder,
			config.DisplayHintAnnotation:    status.DisplayHintSecurityLevel,
			config.CategoryAnnotation:       "高级",
		},
		Migrations: []config.MigrationFunc{status.MigrateSecurityLevelToBoolean},
	})
	if err != nil {
		return err
	}
	cfgOptionDisableAutoPermit = config.Concurrent.GetAsBool(CfgOptionDisableAutoPermitKey, true)
	cfgBoolOptions[CfgOptionDisableAutoPermitKey] = cfgOptionDisableAutoPermit

	// Enable History
	err = config.Register(&config.Option{
		Name: "启用网络历史",
		Key:  CfgOptionEnableHistoryKey,
		Description: `将连接保存到（磁盘上的）数据库中，以便日后查看和搜索。更改可能需要几分钟才能应用到所有连接。

为了减少干扰并优化性能，内部连接和仅限本设备（localhost）的连接不会保存到历史记录中。`,
		OptType:        config.OptTypeBool,
		ReleaseLevel:   config.ReleaseLevelStable,
		ExpertiseLevel: config.ExpertiseLevelUser,
		DefaultValue:   false,
		Annotations: config.Annotations{
			config.SettablePerAppAnnotation:    true,
			config.DisplayOrderAnnotation:      cfgOptionEnableHistoryOrder,
			config.CategoryAnnotation:          "常规",
			config.RequiresFeatureIDAnnotation: account.FeatureHistory,
		},
	})
	if err != nil {
		return err
	}
	cfgOptionEnableHistory = config.Concurrent.GetAsBool(CfgOptionEnableHistoryKey, false)
	cfgBoolOptions[CfgOptionEnableHistoryKey] = cfgOptionEnableHistory

	err = config.Register(&config.Option{
		Name: "保留网络历史",
		Key:  CfgOptionKeepHistoryKey,
		Description: `指定网络历史数据保留的天数。请注意，可用的历史数据越多，报告（即将推出）就越有用。
		
较旧的数据会定期删除，并持续从数据库中清除。如需立即清除已删除的条目，请关闭或重启 Portmaster。

设置为 0 天表示永久保留网络历史。根据您的设备情况，这可能会影响性能。`,
		OptType:        config.OptTypeInt,
		ReleaseLevel:   config.ReleaseLevelStable,
		ExpertiseLevel: config.ExpertiseLevelUser,
		DefaultValue:   30,
		Annotations: config.Annotations{
			config.SettablePerAppAnnotation:    true,
			config.UnitAnnotation:              "天",
			config.DisplayOrderAnnotation:      cfgOptionKeepHistoryOrder,
			config.CategoryAnnotation:          "常规",
			config.RequiresFeatureIDAnnotation: account.FeatureHistory,
		},
	})
	if err != nil {
		return err
	}
	cfgOptionKeepHistory = config.Concurrent.GetAsInt(CfgOptionKeepHistoryKey, 30)
	cfgIntOptions[CfgOptionKeepHistoryKey] = cfgOptionKeepHistory

	rulesHelp := strings.ReplaceAll(`规则按从上到下的顺序检查，在第一个匹配项处停止。规则可以按以下方式匹配：

- 按地址："192.168.0.1"
- 按网络："192.168.0.0/24"
- 按网络范围："Localhost"、"LAN" 或 "Internet"
- 按域名：
	- 匹配特定域名："example.com"
	- 匹配域名及其子域名：".example.com"
	- 使用通配符前缀匹配："*xample.com"
	- 使用通配符后缀匹配："example.*"
	- 匹配包含指定文本的域名："*example*"
- 按国家/地区（基于 IP）："US"（[依据 ISO 3166-1 alpha-2 的两字母国家/地区代码](https://en.wikipedia.org/wiki/ISO_3166-1_alpha-2)）
- 按大洲（基于 IP）："C:US"（在 "AF"、"AN"、"AS"、"EU"、"NA"、"OC" 或 "SA" 前加上前缀 "C:"）
- 按 AS 编号："AS123456"
- 按过滤列表——使用带 "L:" 前缀的过滤列表 ID："L:MAL"
- 匹配任意内容："*"

此外，您还可以使用以下格式指定协议和端口："<主机> <IP 协议>/<端口>"。

协议和端口可以使用数字（"6/80"）或名称（"TCP/HTTP"）指定。  
端口范围使用连字符定义（"TCP/1-1024"）。省略端口则匹配任意端口。  
使用 "*" 匹配任意协议。如果使用任意协议匹配端口，则没有端口的协议不会被匹配。  
带有协议和端口定义的规则，只有在协议和端口也匹配时才会生效。  
端口始终与目标端口进行比较，因此对于入站连接，比较的是本地监听端口。  

示例：
- "192.168.0.1 TCP/HTTP"
- "LAN UDP/50000-55000"
- "example.com */HTTPS"
- "1.1.1.1 ICMP"

重要提示：DNS 请求仅与域名和过滤列表规则进行匹配，其他所有规则都需要 IP 地址，因此只会在随后的 IP 连接中进行检查。

专业提示：您可以使用 "#" 为规则添加注释。
`, `"`, "`")

	// rulesVerdictNames defines the verdicts names to be used for filter rules.
	rulesVerdictNames := map[string]string{
		"-": "阻止", // Default.
		"+": "允许",
	}

	// Endpoint Filter List
	err = config.Register(&config.Option{
		Name:         "出站规则",
		Key:          CfgOptionEndpointsKey,
		Description:  "适用于出站网络连接的规则。无法覆盖网络范围和连接类型设置（见上文）。",
		Help:         rulesHelp,
		Sensitive:    true,
		OptType:      config.OptTypeStringArray,
		DefaultValue: []string{},
		Annotations: config.Annotations{
			config.SettablePerAppAnnotation:              true,
			config.StackableAnnotation:                   true,
			config.DisplayHintAnnotation:                 endpoints.DisplayHintEndpointList,
			config.DisplayOrderAnnotation:                cfgOptionEndpointsOrder,
			config.CategoryAnnotation:                    "规则",
			endpoints.EndpointListVerdictNamesAnnotation: rulesVerdictNames,
		},
		ValidationRegex: endpoints.ListEntryValidationRegex,
		ValidationFunc:  endpoints.ValidateEndpointListConfigOption,
	})
	if err != nil {
		return err
	}
	cfgOptionEndpoints = config.Concurrent.GetAsStringArray(CfgOptionEndpointsKey, []string{})
	cfgStringArrayOptions[CfgOptionEndpointsKey] = cfgOptionEndpoints

	// Service Endpoint Filter List
	err = config.Register(&config.Option{
		Name:           "入站规则",
		Key:            CfgOptionServiceEndpointsKey,
		Description:    "适用于入站网络连接的规则。无法覆盖网络范围和连接类型设置（见上文）。",
		Help:           rulesHelp,
		Sensitive:      true,
		OptType:        config.OptTypeStringArray,
		DefaultValue:   []string{},
		ExpertiseLevel: config.ExpertiseLevelExpert,
		Annotations: config.Annotations{
			config.SettablePerAppAnnotation:              true,
			config.StackableAnnotation:                   true,
			config.DisplayHintAnnotation:                 endpoints.DisplayHintEndpointList,
			config.DisplayOrderAnnotation:                cfgOptionServiceEndpointsOrder,
			config.CategoryAnnotation:                    "规则",
			endpoints.EndpointListVerdictNamesAnnotation: rulesVerdictNames,
			config.QuickSettingsAnnotation: []config.QuickSetting{
				{
					Name:   "允许 SSH",
					Action: config.QuickMergeTop,
					Value:  []string{"+ * tcp/22"},
				},
				{
					Name:   "允许 HTTP/s",
					Action: config.QuickMergeTop,
					Value:  []string{"+ * tcp/80", "+ * tcp/443"},
				},
				{
					Name:   "允许 RDP",
					Action: config.QuickMergeTop,
					Value:  []string{"+ * */3389"},
				},
				{
					Name:   "允许来自局域网的所有连接",
					Action: config.QuickMergeTop,
					Value:  []string{"+ LAN"},
				},
				{
					Name:   "允许来自互联网的所有连接",
					Action: config.QuickMergeTop,
					Value:  []string{"+ Internet"},
				},
				{
					Name:   "阻止其他所有连接",
					Action: config.QuickMergeBottom,
					Value:  []string{"- *"},
				},
			},
		},
		ValidationRegex: endpoints.ListEntryValidationRegex,
		ValidationFunc:  endpoints.ValidateEndpointListConfigOption,
	})
	if err != nil {
		return err
	}
	cfgOptionServiceEndpoints = config.Concurrent.GetAsStringArray(CfgOptionServiceEndpointsKey, []string{})
	cfgStringArrayOptions[CfgOptionServiceEndpointsKey] = cfgOptionServiceEndpoints

	// Filter list IDs
	defaultFilterListsValue := []string{"TRAC", "MAL", "BAD", "UNBREAK"}
	err = config.Register(&config.Option{
		Name:         "过滤列表",
		Key:          CfgOptionFilterListsKey,
		Description:  "阻止与已启用的过滤列表匹配的连接。",
		OptType:      config.OptTypeStringArray,
		DefaultValue: defaultFilterListsValue,
		Annotations: config.Annotations{
			config.SettablePerAppAnnotation: true,
			config.DisplayHintAnnotation:    "filter list",
			config.DisplayOrderAnnotation:   cfgOptionFilterListsOrder,
			config.CategoryAnnotation:       "过滤列表",
		},
		ValidationRegex: `^[a-zA-Z0-9\-]+$`,
	})
	if err != nil {
		return err
	}
	cfgOptionFilterLists = config.Concurrent.GetAsStringArray(CfgOptionFilterListsKey, defaultFilterListsValue)
	cfgStringArrayOptions[CfgOptionFilterListsKey] = cfgOptionFilterLists

	// Include CNAMEs
	err = config.Register(&config.Option{
		Name:           "阻止域名别名",
		Key:            CfgOptionFilterCNAMEKey,
		Description:    "如果解析出的 CNAME（别名）被规则或过滤列表阻止，则同时阻止该域名。",
		OptType:        config.OptTypeBool,
		DefaultValue:   true,
		ExpertiseLevel: config.ExpertiseLevelExpert,
		Annotations: config.Annotations{
			config.SettablePerAppAnnotation: true,
			config.DisplayHintAnnotation:    status.DisplayHintSecurityLevel,
			config.DisplayOrderAnnotation:   cfgOptionFilterCNAMEOrder,
			config.CategoryAnnotation:       "DNS 过滤",
		},
		Migrations: []config.MigrationFunc{status.MigrateSecurityLevelToBoolean},
	})
	if err != nil {
		return err
	}
	cfgOptionFilterCNAME = config.Concurrent.GetAsBool(CfgOptionFilterCNAMEKey, true)
	cfgBoolOptions[CfgOptionFilterCNAMEKey] = cfgOptionFilterCNAME

	// Include subdomains
	err = config.Register(&config.Option{
		Name:         "阻止过滤列表条目的子域名",
		Key:          CfgOptionFilterSubDomainsKey,
		Description:  "额外阻止所选过滤列表中条目的所有子域名。",
		OptType:      config.OptTypeBool,
		DefaultValue: true,
		Annotations: config.Annotations{
			config.SettablePerAppAnnotation: true,
			config.DisplayHintAnnotation:    status.DisplayHintSecurityLevel,
			config.DisplayOrderAnnotation:   cfgOptionFilterSubDomainsOrder,
			config.CategoryAnnotation:       "过滤列表",
		},
		Migrations: []config.MigrationFunc{status.MigrateSecurityLevelToBoolean},
	})
	if err != nil {
		return err
	}
	cfgOptionFilterSubDomains = config.Concurrent.GetAsBool(CfgOptionFilterSubDomainsKey, true)
	cfgBoolOptions[CfgOptionFilterSubDomainsKey] = cfgOptionFilterSubDomains

	// Block Scope Local
	err = config.Register(&config.Option{
		Name:           "强制阻止本设备内部连接",
		Key:            CfgOptionBlockScopeLocalKey,
		Description:    "强制阻止您设备上的所有内部连接，即 localhost。优先级高于规则（见下文）。",
		OptType:        config.OptTypeBool,
		ExpertiseLevel: config.ExpertiseLevelExpert,
		DefaultValue:   false,
		Annotations: config.Annotations{
			config.SettablePerAppAnnotation: true,
			config.DisplayHintAnnotation:    status.DisplayHintSecurityLevel,
			config.DisplayOrderAnnotation:   cfgOptionBlockScopeLocalOrder,
			config.CategoryAnnotation:       "网络范围",
		},
		Migrations: []config.MigrationFunc{status.MigrateSecurityLevelToBoolean},
	})
	if err != nil {
		return err
	}
	cfgOptionBlockScopeLocal = config.Concurrent.GetAsBool(CfgOptionBlockScopeLocalKey, false)
	cfgBoolOptions[CfgOptionBlockScopeLocalKey] = cfgOptionBlockScopeLocal

	// Block Scope LAN
	err = config.Register(&config.Option{
		Name:         "强制阻止局域网",
		Key:          CfgOptionBlockScopeLANKey,
		Description:  "强制阻止所有来自和发往局域网的连接。优先级高于规则（见下文）。",
		OptType:      config.OptTypeBool,
		DefaultValue: false,
		Annotations: config.Annotations{
			config.SettablePerAppAnnotation: true,
			config.DisplayHintAnnotation:    status.DisplayHintSecurityLevel,
			config.DisplayOrderAnnotation:   cfgOptionBlockScopeLANOrder,
			config.CategoryAnnotation:       "网络范围",
		},
		Migrations: []config.MigrationFunc{status.MigrateSecurityLevelToBoolean},
	})
	if err != nil {
		return err
	}
	cfgOptionBlockScopeLAN = config.Concurrent.GetAsBool(CfgOptionBlockScopeLANKey, false)
	cfgBoolOptions[CfgOptionBlockScopeLANKey] = cfgOptionBlockScopeLAN

	// Block Scope Internet
	err = config.Register(&config.Option{
		Name:         "强制阻止互联网访问",
		Key:          CfgOptionBlockScopeInternetKey,
		Description:  "强制阻止所有来自和发往互联网的连接。优先级高于规则（见下文）。",
		OptType:      config.OptTypeBool,
		DefaultValue: false,
		Annotations: config.Annotations{
			config.SettablePerAppAnnotation: true,
			config.DisplayHintAnnotation:    status.DisplayHintSecurityLevel,
			config.DisplayOrderAnnotation:   cfgOptionBlockScopeInternetOrder,
			config.CategoryAnnotation:       "网络范围",
		},
		Migrations: []config.MigrationFunc{status.MigrateSecurityLevelToBoolean},
	})
	if err != nil {
		return err
	}
	cfgOptionBlockScopeInternet = config.Concurrent.GetAsBool(CfgOptionBlockScopeInternetKey, false)
	cfgBoolOptions[CfgOptionBlockScopeInternetKey] = cfgOptionBlockScopeInternet

	// Block Peer to Peer Connections
	err = config.Register(&config.Option{
		Name:         "强制阻止 P2P/直接连接",
		Key:          CfgOptionBlockP2PKey,
		Description:  "指未先通过 DNS 解析域名、而直接与互联网上的 IP 地址或对等方建立的连接。优先级高于规则（见下文）。",
		OptType:      config.OptTypeBool,
		DefaultValue: false,
		Annotations: config.Annotations{
			config.SettablePerAppAnnotation: true,
			config.DisplayHintAnnotation:    status.DisplayHintSecurityLevel,
			config.DisplayOrderAnnotation:   cfgOptionBlockP2POrder,
			config.CategoryAnnotation:       "连接类型",
		},
		Migrations: []config.MigrationFunc{status.MigrateSecurityLevelToBoolean},
	})
	if err != nil {
		return err
	}
	cfgOptionBlockP2P = config.Concurrent.GetAsBool(CfgOptionBlockP2PKey, false)
	cfgBoolOptions[CfgOptionBlockP2PKey] = cfgOptionBlockP2P

	// Block Inbound Connections
	err = config.Register(&config.Option{
		Name:         "强制阻止入站连接",
		Key:          CfgOptionBlockInboundKey,
		Description:  "指从局域网或互联网发起、指向您设备的连接。通常只有在您运行网络服务或使用 P2P 软件时才会出现。优先级高于规则（见下文）。",
		OptType:      config.OptTypeBool,
		DefaultValue: true,
		Annotations: config.Annotations{
			config.SettablePerAppAnnotation: true,
			config.DisplayHintAnnotation:    status.DisplayHintSecurityLevel,
			config.DisplayOrderAnnotation:   cfgOptionBlockInboundOrder,
			config.CategoryAnnotation:       "连接类型",
		},
		Migrations: []config.MigrationFunc{status.MigrateSecurityLevelToBoolean},
	})
	if err != nil {
		return err
	}
	cfgOptionBlockInbound = config.Concurrent.GetAsBool(CfgOptionBlockInboundKey, false)
	cfgBoolOptions[CfgOptionBlockInboundKey] = cfgOptionBlockInbound

	// Filter Out-of-Scope DNS Records
	err = config.Register(&config.Option{
		Name:           "强制公网/私网分离视图",
		Key:            CfgOptionRemoveOutOfScopeDNSKey,
		Description:    "拒绝公共 DNS 响应中的私有 IP 地址（RFC1918 等）。如果正在使用系统解析器，则会阻止随后产生的连接，而不是 DNS 请求。",
		OptType:        config.OptTypeBool,
		ExpertiseLevel: config.ExpertiseLevelDeveloper,
		DefaultValue:   true,
		Annotations: config.Annotations{
			config.SettablePerAppAnnotation: true,
			config.DisplayHintAnnotation:    status.DisplayHintSecurityLevel,
			config.DisplayOrderAnnotation:   cfgOptionRemoveOutOfScopeDNSOrder,
			config.CategoryAnnotation:       "DNS 过滤",
		},
		Migrations: []config.MigrationFunc{status.MigrateSecurityLevelToBoolean},
	})
	if err != nil {
		return err
	}
	cfgOptionRemoveOutOfScopeDNS = config.Concurrent.GetAsBool(CfgOptionRemoveOutOfScopeDNSKey, true)
	cfgBoolOptions[CfgOptionRemoveOutOfScopeDNSKey] = cfgOptionRemoveOutOfScopeDNS

	// Filter DNS Records that would be blocked
	err = config.Register(&config.Option{
		Name:           "拒绝被阻止的 IP",
		Key:            CfgOptionRemoveBlockedDNSKey,
		Description:    "直接从 DNS 响应中剔除被阻止的 IP 地址，而不是先交给应用再阻止随后产生的连接。此设置不影响隐私，且仅在未使用系统解析器时生效。",
		OptType:        config.OptTypeBool,
		ExpertiseLevel: config.ExpertiseLevelDeveloper,
		DefaultValue:   true,
		Annotations: config.Annotations{
			config.SettablePerAppAnnotation: true,
			config.DisplayHintAnnotation:    status.DisplayHintSecurityLevel,
			config.DisplayOrderAnnotation:   cfgOptionRemoveBlockedDNSOrder,
			config.CategoryAnnotation:       "DNS 过滤",
		},
		Migrations: []config.MigrationFunc{status.MigrateSecurityLevelToBoolean},
	})
	if err != nil {
		return err
	}
	cfgOptionRemoveBlockedDNS = config.Concurrent.GetAsBool(CfgOptionRemoveBlockedDNSKey, true)
	cfgBoolOptions[CfgOptionRemoveBlockedDNSKey] = cfgOptionRemoveBlockedDNS

	// Domain heuristics
	err = config.Register(&config.Option{
		Name:           "启用域名启发式检测",
		Key:            CfgOptionDomainHeuristicsKey,
		Description:    "检查可疑域名并将其阻止。此选项目前针对由恶意软件生成的域名以及 DNS 数据外泄通道。",
		OptType:        config.OptTypeBool,
		ExpertiseLevel: config.ExpertiseLevelExpert,
		DefaultValue:   true,
		Annotations: config.Annotations{
			config.SettablePerAppAnnotation: true,
			config.DisplayHintAnnotation:    status.DisplayHintSecurityLevel,
			config.DisplayOrderAnnotation:   cfgOptionDomainHeuristicsOrder,
			config.CategoryAnnotation:       "DNS 过滤",
		},
		Migrations: []config.MigrationFunc{status.MigrateSecurityLevelToBoolean},
	})
	if err != nil {
		return err
	}
	cfgOptionDomainHeuristics = config.Concurrent.GetAsBool(CfgOptionDomainHeuristicsKey, true)
	cfgBoolOptions[CfgOptionDomainHeuristicsKey] = cfgOptionDomainHeuristics

	// Bypass prevention
	err = config.Register(&config.Option{
		Name: "阻止绕过安全 DNS",
		Key:  CfgOptionPreventBypassingKey,
		Description: `防止应用绕过 Portmaster 的安全 DNS 解析器。
如果禁用，Portmaster 可能缺少足够的信息来正确执行规则和过滤列表。
重要提示：Portmaster 的防火墙本身无法被绕过。

当前功能：  
- 禁用 Firefox 内置的 DNS-over-HTTPS 解析器
- 阻止直接访问公共 DNS 解析器

请注意，DNS 绕过尝试还可能在“System DNS Client”应用中被额外阻止。`,
		OptType:        config.OptTypeBool,
		ExpertiseLevel: config.ExpertiseLevelUser,
		ReleaseLevel:   config.ReleaseLevelStable,
		DefaultValue:   true,
		Annotations: config.Annotations{
			config.SettablePerAppAnnotation: true,
			config.DisplayHintAnnotation:    status.DisplayHintSecurityLevel,
			config.DisplayOrderAnnotation:   cfgOptionPreventBypassingOrder,
			config.CategoryAnnotation:       "高级",
		},
		Migrations: []config.MigrationFunc{status.MigrateSecurityLevelToBoolean},
	})
	if err != nil {
		return err
	}
	cfgOptionPreventBypassing = config.Concurrent.GetAsBool(CfgOptionPreventBypassingKey, true)
	cfgBoolOptions[CfgOptionPreventBypassingKey] = cfgOptionPreventBypassing

	// Use SPN
	err = config.Register(&config.Option{
		Name:         "使用 SPN",
		Key:          CfgOptionUseSPNKey,
		Description:  "使用 Safing 隐私网络（SPN）保护网络流量。如果 SPN 不可用或连接中断，网络流量将被阻止。",
		OptType:      config.OptTypeBool,
		DefaultValue: true,
		Annotations: config.Annotations{
			config.SettablePerAppAnnotation: true,
			config.DisplayOrderAnnotation:   cfgOptionUseSPNOrder,
			config.CategoryAnnotation:       "常规",
		},
	})
	if err != nil {
		return err
	}
	cfgOptionUseSPN = config.Concurrent.GetAsBool(CfgOptionUseSPNKey, true)
	cfgBoolOptions[CfgOptionUseSPNKey] = cfgOptionUseSPN

	// SPN Rules
	err = config.Register(&config.Option{
		Name:         "SPN 规则",
		Key:          CfgOptionSPNUsagePolicyKey,
		Description:  `自定义哪些连接应当或不应当通过 SPN 路由的规则。仅在启用“使用 SPN”时生效。`,
		Help:         rulesHelp,
		Sensitive:    true,
		OptType:      config.OptTypeStringArray,
		DefaultValue: []string{},
		Annotations: config.Annotations{
			config.SettablePerAppAnnotation:              true,
			config.StackableAnnotation:                   true,
			config.CategoryAnnotation:                    "常规",
			config.DisplayOrderAnnotation:                cfgOptionSPNUsagePolicyOrder,
			config.DisplayHintAnnotation:                 endpoints.DisplayHintEndpointList,
			endpoints.EndpointListVerdictNamesAnnotation: SPNRulesVerdictNames,
		},
		ValidationRegex: endpoints.ListEntryValidationRegex,
		ValidationFunc:  endpoints.ValidateEndpointListConfigOption,
	})
	if err != nil {
		return err
	}
	cfgOptionSPNUsagePolicy = config.Concurrent.GetAsStringArray(CfgOptionSPNUsagePolicyKey, []string{})
	cfgStringArrayOptions[CfgOptionSPNUsagePolicyKey] = cfgOptionSPNUsagePolicy

	// Transit Node Rules
	err = config.Register(&config.Option{
		Name:           "中转节点规则",
		Key:            CfgOptionTransitHubPolicyKey,
		Description:    `自定义哪些国家/地区应当或不应当用作中转节点。中转节点用于在 SPN 中从您的主节点中转到出口节点。`,
		Help:           SPNRulesHelp,
		Sensitive:      true,
		OptType:        config.OptTypeStringArray,
		ExpertiseLevel: config.ExpertiseLevelExpert,
		DefaultValue:   []string{},
		Annotations: config.Annotations{
			config.SettablePerAppAnnotation:              true,
			config.StackableAnnotation:                   true,
			config.CategoryAnnotation:                    "路由",
			config.DisplayOrderAnnotation:                cfgOptionTransitHubPolicyOrder,
			config.DisplayHintAnnotation:                 endpoints.DisplayHintEndpointList,
			config.QuickSettingsAnnotation:               SPNRulesQuickSettings,
			endpoints.EndpointListVerdictNamesAnnotation: SPNRulesVerdictNames,
		},
		ValidationRegex: endpoints.ListEntryValidationRegex,
		ValidationFunc:  endpoints.ValidateEndpointListConfigOption,
	})
	if err != nil {
		return err
	}
	cfgOptionTransitHubPolicy = config.Concurrent.GetAsStringArray(CfgOptionTransitHubPolicyKey, []string{})
	cfgStringArrayOptions[CfgOptionTransitHubPolicyKey] = cfgOptionTransitHubPolicy

	// Exit Node Rules
	err = config.Register(&config.Option{
		Name: "出口节点规则",
		Key:  CfgOptionExitHubPolicyKey,
		Description: `自定义哪些国家/地区应当或不应当用作出口节点。出口节点用于离开 SPN 并与您的目标建立连接。

默认情况下，Portmaster 会尝试选择离目标最近的节点作为出口节点，以减少您在开放互联网上的暴露。每个目标都会单独选择出口节点。`,
		Help:         SPNRulesHelp,
		Sensitive:    true,
		OptType:      config.OptTypeStringArray,
		DefaultValue: []string{},
		Annotations: config.Annotations{
			config.SettablePerAppAnnotation:              true,
			config.StackableAnnotation:                   true,
			config.CategoryAnnotation:                    "路由",
			config.DisplayOrderAnnotation:                cfgOptionExitHubPolicyOrder,
			config.DisplayHintAnnotation:                 endpoints.DisplayHintEndpointList,
			config.QuickSettingsAnnotation:               SPNRulesQuickSettings,
			endpoints.EndpointListVerdictNamesAnnotation: SPNRulesVerdictNames,
		},
		ValidationRegex: endpoints.ListEntryValidationRegex,
		ValidationFunc:  endpoints.ValidateEndpointListConfigOption,
	})
	if err != nil {
		return err
	}
	cfgOptionExitHubPolicy = config.Concurrent.GetAsStringArray(CfgOptionExitHubPolicyKey, []string{})
	cfgStringArrayOptions[CfgOptionExitHubPolicyKey] = cfgOptionExitHubPolicy

	// Select SPN Routing Algorithm
	err = config.Register(&config.Option{
		Name:         "选择 SPN 路由算法",
		Key:          CfgOptionRoutingAlgorithmKey,
		Description:  "为通过 SPN 的连接选择路由算法，在速度与隐私之间设置您偏好的平衡。必要时，Portmaster 可能会自动升级路由算法以保护您的隐私。",
		OptType:      config.OptTypeString,
		DefaultValue: DefaultRoutingProfileID,
		Annotations: config.Annotations{
			config.SettablePerAppAnnotation: true,
			config.DisplayHintAnnotation:    config.DisplayHintOneOf,
			config.DisplayOrderAnnotation:   cfgOptionRoutingAlgorithmOrder,
			config.CategoryAnnotation:       "路由",
		},
		PossibleValues: []config.PossibleValue{
			{
				Name:        "普通 VPN 模式",
				Value:       "home",
				Description: "始终从主节点直接连接到目标。仅提供非常基础的隐私保护，因为主节点既知道您来自哪里，也知道您要连接到哪里。",
			},
			{
				Name:        "速度优先",
				Value:       "single-hop",
				Description: "以至少一跳优化路由，提供良好的速度。对于离您较近的目标通常会使用主节点直接连接，而对于较远的目标会使用更多跳数，以在长距离上获得更好的隐私。",
			},
			{
				Name:        "均衡",
				Value:       "double-hop",
				Description: "以至少两跳优化路由，兼顾良好的隐私和速度。没有任何单个节点能同时知道您来自哪里*以及*您要连接到哪里。",
			},
			{
				Name:        "隐私优先",
				Value:       "triple-hop",
				Description: "以至少三跳优化路由，提供非常好的隐私保护。没有任何单个节点能同时知道您来自哪里*以及*您要连接到哪里——并额外增加一跳以确保万无一失。",
			},
		},
	})
	if err != nil {
		return err
	}
	cfgOptionRoutingAlgorithm = config.Concurrent.GetAsString(CfgOptionRoutingAlgorithmKey, DefaultRoutingProfileID)
	cfgStringOptions[CfgOptionRoutingAlgorithmKey] = cfgOptionRoutingAlgorithm

	//
	// Split Tunnel
	//

	// Split Tunnel: Use
	err = config.Register(&config.Option{
		Name: "使用分离隧道",
		Key:  CfgOptionSplitTunUseKey,
		Description: `将特定流量通过其他网络接口路由，绕过系统默认路由（适用于让某些应用不经过 VPN）。

启用此选项且“网络接口”选项为空时，Portmaster 会尝试通过默认的物理网络接口路由您的流量。

重要提示：SPN 优先于分离隧道。若要将分离隧道与 SPN 一起使用，请按应用配置 SPN，或定义允许分离隧道生效的例外规则。`,
		OptType:      config.OptTypeBool,
		DefaultValue: false,
		Annotations: config.Annotations{
			config.SettablePerAppAnnotation: true,
			config.DisplayOrderAnnotation:   cfgOptionSplitTunUseOrder,
			config.CategoryAnnotation:       "常规",
		},
	})
	if err != nil {
		return err
	}
	cfgOptionSplitTunUse = config.Concurrent.GetAsBool(CfgOptionSplitTunUseKey, false)
	cfgBoolOptions[CfgOptionSplitTunUseKey] = cfgOptionSplitTunUse

	// Split Tunnel: Network Interface
	err = config.Register(&config.Option{
		Name: "网络接口",
		Key:  CfgOptionSplitTunInterfaceKey,
		Description: `指定用于路由分离隧道流量的网络接口。可以通过以下方式定义：
- 接口名称："Ethernet"、"Wi-Fi"、"wlan0" 等
- 接口 IP 地址："192.168.1.1"、"10.0.0.1" 等
- 接口 MAC 地址："00:1A:2B:3C:4D:5E"、"01:23:45:67:89:AB" 等

留空则由 Portmaster 自动检测物理网络接口并忽略虚拟 VPN 接口，这有助于绕过 VPN 隧道。如果留空无法按预期工作，可以手动指定接口以提高可靠性。

重要提示：如果无法检测到网络接口或网络接口不可用，连接将被丢弃。

重要提示：SPN 优先于分离隧道。若要将分离隧道与 SPN 一起使用，请按应用配置 SPN，或定义允许分离隧道生效的例外规则。`,
		Sensitive:    true,
		OptType:      config.OptTypeString,
		DefaultValue: "",
		Annotations: config.Annotations{
			config.SettablePerAppAnnotation: true,
			config.DisplayOrderAnnotation:   cfgOptionSplitTunInterfaceOrder,
			config.CategoryAnnotation:       "常规",
		},
		ValidationFunc: func(value interface{}) error {
			if s, ok := value.(string); ok && s != "" && strings.TrimSpace(s) == "" {
				return errors.New("network interface cannot contain only whitespace characters")
			}
			return nil
		},
	})
	if err != nil {
		return err
	}
	cfgOptionSplitTunInterface = config.Concurrent.GetAsString(CfgOptionSplitTunInterfaceKey, "")
	cfgStringOptions[CfgOptionSplitTunInterfaceKey] = cfgOptionSplitTunInterface

	// Split Tunnel: Rules
	splitTunRulesVerdictNames := map[string]string{
		"-": "排除", // Default.
		"+": "允许",
	}

	err = config.Register(&config.Option{
		Name: "分离隧道规则",
		Key:  CfgOptionSplitTunUsagePolicyKey,
		Description: `自定义哪些连接应当或不应当通过分离隧道路由的规则。仅在启用“使用分离隧道”时生效。
		
重要提示：SPN 优先于分离隧道。若要将分离隧道与 SPN 一起使用，请按应用配置 SPN，或定义允许分离隧道生效的例外规则。`,
		Help:         rulesHelp,
		Sensitive:    true,
		OptType:      config.OptTypeStringArray,
		DefaultValue: []string{},
		Annotations: config.Annotations{
			config.SettablePerAppAnnotation:              true,
			config.StackableAnnotation:                   true,
			config.CategoryAnnotation:                    "常规",
			config.DisplayOrderAnnotation:                cfgOptionSplitTunUsagePolicyOrder,
			config.DisplayHintAnnotation:                 endpoints.DisplayHintEndpointList,
			endpoints.EndpointListVerdictNamesAnnotation: splitTunRulesVerdictNames,
		},
		ValidationRegex: endpoints.ListEntryValidationRegex,
		ValidationFunc:  endpoints.ValidateEndpointListConfigOption,
	})
	if err != nil {
		return err
	}
	cfgOptionSplitTunUsagePolicy = config.Concurrent.GetAsStringArray(CfgOptionSplitTunUsagePolicyKey, []string{})
	cfgStringArrayOptions[CfgOptionSplitTunUsagePolicyKey] = cfgOptionSplitTunUsagePolicy

	return nil
}
