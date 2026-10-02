package captain

import (
	"sync"

	"github.com/safing/portmaster/base/config"
	"github.com/safing/portmaster/service/profile"
	"github.com/safing/portmaster/service/profile/endpoints"
	"github.com/safing/portmaster/spn/conf"
	"github.com/safing/portmaster/spn/navigator"
)

var (
	// CfgOptionEnableSPNKey is the configuration key for the SPN module.
	CfgOptionEnableSPNKey   = "spn/enable"
	cfgOptionEnableSPNOrder = 128

	// CfgOptionHomeHubPolicyKey is the configuration key for the SPN home policy.
	CfgOptionHomeHubPolicyKey   = "spn/homePolicy"
	cfgOptionHomeHubPolicy      config.StringArrayOption
	cfgOptionHomeHubPolicyOrder = 145

	// CfgOptionDNSExitHubPolicyKey is the configuration key for the SPN DNS exit policy.
	CfgOptionDNSExitHubPolicyKey   = "spn/dnsExitPolicy"
	cfgOptionDNSExitHubPolicy      config.StringArrayOption
	cfgOptionDNSExitHubPolicyOrder = 148

	// CfgOptionUseCommunityNodesKey is the configuration key for whether to use community nodes.
	CfgOptionUseCommunityNodesKey   = "spn/useCommunityNodes"
	cfgOptionUseCommunityNodes      config.BoolOption
	cfgOptionUseCommunityNodesOrder = 149

	// NonCommunityVerifiedOwners holds a list of verified owners that are not
	// considered "community".
	NonCommunityVerifiedOwners = []string{"Safing"}

	// CfgOptionTrustNodeNodesKey is the configuration key for whether additional trusted nodes.
	CfgOptionTrustNodeNodesKey   = "spn/trustNodes"
	cfgOptionTrustNodeNodes      config.StringArrayOption
	cfgOptionTrustNodeNodesOrder = 150

	// Special Access Code.
	cfgOptionSpecialAccessCodeKey     = "spn/specialAccessCode"
	cfgOptionSpecialAccessCodeDefault = "none"
	cfgOptionSpecialAccessCode        config.StringOption //nolint:unused // Linter, you drunk?
	cfgOptionSpecialAccessCodeOrder   = 160

	// IPv6 must be global and accessible.
	cfgOptionBindToAdvertisedKey     = "spn/publicHub/bindToAdvertised"
	cfgOptionBindToAdvertised        config.BoolOption
	cfgOptionBindToAdvertisedDefault = false
	cfgOptionBindToAdvertisedOrder   = 161

	// Config options for use.
	cfgOptionRoutingAlgorithm config.StringOption
)

func prepConfig() error {
	// Register spn module setting.
	err := config.Register(&config.Option{
		Name:         "SPN 模块",
		Key:          CfgOptionEnableSPNKey,
		Description:  "启动 Safing 隐私网络（SPN）模块。如果关闭，SPN 将在此设备上完全禁用。",
		OptType:      config.OptTypeBool,
		DefaultValue: false,
		Annotations: config.Annotations{
			config.DisplayOrderAnnotation: cfgOptionEnableSPNOrder,
			config.CategoryAnnotation:     "常规",
		},
	})
	if err != nil {
		return err
	}

	// Home Node Rules
	err = config.Register(&config.Option{
		Name: "主节点规则",
		Key:  CfgOptionHomeHubPolicyKey,
		Description: `自定义哪些国家/地区应该或不应该用作您的主节点。主节点是您进入 SPN 的入口。您直接连接到它，您的所有连接都会经由它路由。

默认情况下，Portmaster 会尝试选择最近的节点作为您的主节点，以减少您在开放互联网上的暴露。

重新连接 SPN 以应用新规则。`,
		Help:            profile.SPNRulesHelp,
		Sensitive:       true,
		OptType:         config.OptTypeStringArray,
		RequiresRestart: true,
		ExpertiseLevel:  config.ExpertiseLevelExpert,
		DefaultValue:    []string{},
		Annotations: config.Annotations{
			config.CategoryAnnotation:                    "路由",
			config.DisplayOrderAnnotation:                cfgOptionHomeHubPolicyOrder,
			config.DisplayHintAnnotation:                 endpoints.DisplayHintEndpointList,
			config.QuickSettingsAnnotation:               profile.SPNRulesQuickSettings,
			endpoints.EndpointListVerdictNamesAnnotation: profile.SPNRulesVerdictNames,
		},
		ValidationRegex: endpoints.ListEntryValidationRegex,
		ValidationFunc:  endpoints.ValidateEndpointListConfigOption,
	})
	if err != nil {
		return err
	}
	cfgOptionHomeHubPolicy = config.Concurrent.GetAsStringArray(CfgOptionHomeHubPolicyKey, []string{})

	// DNS Exit Node Rules
	err = config.Register(&config.Option{
		Name: "DNS 出口节点规则",
		Key:  CfgOptionDNSExitHubPolicyKey,
		Description: `自定义哪些国家/地区应该或不应该用作 DNS 出口节点。

默认情况下，Portmaster 会直接在您的主节点处发出 DNS 请求，以保持快速并靠近您的位置。这一点很重要，因为 DNS 解析在决定向您返回哪些优化的 DNS 记录时，通常会考虑您的大致位置。由于 Portmaster 默认会加密您的 DNS 请求，您的 DNS 请求实际上获得了两跳级别的安全性，从而保护您的隐私。

此设置主要用于您需要在更底层也模拟自己位于其他位置的情况。这可能是绕过更智能的地理封锁系统所必需的。`,
		Help:            profile.SPNRulesHelp,
		Sensitive:       true,
		OptType:         config.OptTypeStringArray,
		RequiresRestart: true,
		ExpertiseLevel:  config.ExpertiseLevelExpert,
		DefaultValue:    []string{},
		Annotations: config.Annotations{
			config.CategoryAnnotation:                    "路由",
			config.DisplayOrderAnnotation:                cfgOptionDNSExitHubPolicyOrder,
			config.DisplayHintAnnotation:                 endpoints.DisplayHintEndpointList,
			config.QuickSettingsAnnotation:               profile.SPNRulesQuickSettings,
			endpoints.EndpointListVerdictNamesAnnotation: profile.SPNRulesVerdictNames,
		},
		ValidationRegex: endpoints.ListEntryValidationRegex,
		ValidationFunc:  endpoints.ValidateEndpointListConfigOption,
	})
	if err != nil {
		return err
	}
	cfgOptionDNSExitHubPolicy = config.Concurrent.GetAsStringArray(CfgOptionDNSExitHubPolicyKey, []string{})

	err = config.Register(&config.Option{
		Name:            "使用社区节点",
		Key:             CfgOptionUseCommunityNodesKey,
		Description:     "使用并非由 Safing 自己运营的节点（服务器）。建议使用社区节点，因为它能使您的连接所用节点的所有权更加多样化，并进一步增强您的隐私。明文连接（例如 http、smtp 等）永远不会经由社区节点出口，因此可以放心使用此设置。",
		Sensitive:       true,
		OptType:         config.OptTypeBool,
		RequiresRestart: true,
		DefaultValue:    true,
		Annotations: config.Annotations{
			config.DisplayOrderAnnotation: cfgOptionUseCommunityNodesOrder,
			config.CategoryAnnotation:     "路由",
		},
	})
	if err != nil {
		return err
	}
	cfgOptionUseCommunityNodes = config.Concurrent.GetAsBool(CfgOptionUseCommunityNodesKey, true)

	err = config.Register(&config.Option{
		Name:           "信任节点",
		Key:            CfgOptionTrustNodeNodesKey,
		Description:    "指定额外信任哪些社区节点。这些节点随后也可以用作主节点，以及未加密连接的出口节点。",
		Help:           "您可以通过节点 ID 或其经过验证的运营者来指定节点。",
		Sensitive:      true,
		OptType:        config.OptTypeStringArray,
		ExpertiseLevel: config.ExpertiseLevelExpert,
		DefaultValue:   []string{},
		Annotations: config.Annotations{
			config.DisplayOrderAnnotation: cfgOptionTrustNodeNodesOrder,
			config.CategoryAnnotation:     "路由",
		},
	})
	if err != nil {
		return err
	}
	cfgOptionTrustNodeNodes = config.Concurrent.GetAsStringArray(CfgOptionTrustNodeNodesKey, []string{})

	err = config.Register(&config.Option{
		Name:         "特殊访问码",
		Key:          cfgOptionSpecialAccessCodeKey,
		Description:  "特殊访问码可授予出于测试或评估目的访问 SPN 的权限。",
		Sensitive:    true,
		OptType:      config.OptTypeString,
		DefaultValue: cfgOptionSpecialAccessCodeDefault,
		Annotations: config.Annotations{
			config.DisplayOrderAnnotation: cfgOptionSpecialAccessCodeOrder,
			config.CategoryAnnotation:     "高级",
		},
	})
	if err != nil {
		return err
	}
	cfgOptionSpecialAccessCode = config.Concurrent.GetAsString(cfgOptionSpecialAccessCodeKey, "")

	if conf.PublicHub() {
		err = config.Register(&config.Option{
			Name:            "仅从公布的 IP 连接",
			Key:             cfgOptionBindToAdvertisedKey,
			Description:     "仅从公布的 IP 地址发起连接（绑定到这些地址）。",
			OptType:         config.OptTypeBool,
			ExpertiseLevel:  config.ExpertiseLevelExpert,
			DefaultValue:    cfgOptionBindToAdvertisedDefault,
			RequiresRestart: true,
			Annotations: config.Annotations{
				config.DisplayOrderAnnotation: cfgOptionBindToAdvertisedOrder,
			},
		})
		if err != nil {
			return err
		}
		cfgOptionBindToAdvertised = config.GetAsBool(cfgOptionBindToAdvertisedKey, cfgOptionBindToAdvertisedDefault)
	}

	// Config options for use.
	if conf.Integrated() {
		cfgOptionRoutingAlgorithm = config.Concurrent.GetAsString(profile.CfgOptionRoutingAlgorithmKey, navigator.DefaultRoutingProfileID)
	} else {
		cfgOptionRoutingAlgorithm = func() string { return navigator.DefaultRoutingProfileID }
	}

	return nil
}

var (
	homeHubPolicy           endpoints.Endpoints
	homeHubPolicyLock       sync.Mutex
	homeHubPolicyConfigFlag = config.NewValidityFlag()
)

func getHomeHubPolicy() (endpoints.Endpoints, error) {
	homeHubPolicyLock.Lock()
	defer homeHubPolicyLock.Unlock()

	// Return cached value if config is still valid.
	if homeHubPolicyConfigFlag.IsValid() {
		return homeHubPolicy, nil
	}
	homeHubPolicyConfigFlag.Refresh()

	// Parse new policy.
	policy, err := endpoints.ParseEndpoints(cfgOptionHomeHubPolicy())
	if err != nil {
		homeHubPolicy = nil
		return nil, err
	}

	// Save and return the new policy.
	homeHubPolicy = policy
	return homeHubPolicy, nil
}

var (
	dnsExitHubPolicy           endpoints.Endpoints
	dnsExitHubPolicyLock       sync.Mutex
	dnsExitHubPolicyConfigFlag = config.NewValidityFlag()
)

// GetDNSExitHubPolicy return the current DNS exit policy.
func GetDNSExitHubPolicy() (endpoints.Endpoints, error) {
	dnsExitHubPolicyLock.Lock()
	defer dnsExitHubPolicyLock.Unlock()

	// Return cached value if config is still valid.
	if dnsExitHubPolicyConfigFlag.IsValid() {
		return dnsExitHubPolicy, nil
	}
	dnsExitHubPolicyConfigFlag.Refresh()

	// Parse new policy.
	policy, err := endpoints.ParseEndpoints(cfgOptionDNSExitHubPolicy())
	if err != nil {
		dnsExitHubPolicy = nil
		return nil, err
	}

	// Save and return the new policy.
	dnsExitHubPolicy = policy
	return dnsExitHubPolicy, nil
}
