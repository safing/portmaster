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
		Name:         "SPN Module",
		Key:          CfgOptionEnableSPNKey,
		Description:  "Start the Safing Privacy Network module. If turned off, the SPN is fully disabled on this device.",
		OptType:      config.OptTypeBool,
		DefaultValue: false,
		Annotations: config.Annotations{
			config.DisplayOrderAnnotation: cfgOptionEnableSPNOrder,
			config.CategoryAnnotation:     "General",
		},
	})
	if err != nil {
		return err
	}

	// Home Node Rules
	err = config.Register(&config.Option{
		Name: "Home Node Rules",
		Key:  CfgOptionHomeHubPolicyKey,
		Description: `Customize which countries should or should not be used for your Home Node. The Home Node is your entry into the SPN. You connect directly to it and all your connections are routed through it.

By default, the Portmaster tries to choose the nearest node as your Home Node in order to reduce your exposure to the open Internet.

Reconnect to the SPN in order to apply new rules.`,
		Help:            profile.SPNRulesHelp,
		Sensitive:       true,
		OptType:         config.OptTypeStringArray,
		RequiresRestart: true,
		ExpertiseLevel:  config.ExpertiseLevelExpert,
		DefaultValue:    []string{},
		Annotations: config.Annotations{
			config.CategoryAnnotation:                    "Routing",
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
		Name: "DNS Exit Node Rules",
		Key:  CfgOptionDNSExitHubPolicyKey,
		Description: `Customize which countries should or should not be used as DNS Exit Nodes.

By default, the Portmaster will exit DNS requests directly at your Home Node in order to keep them fast and close to your location. This is important, as DNS resolution often takes your approximate location into account when deciding which optimized DNS records are returned to you. As the Portmaster encrypts your DNS requests by default, you effectively gain a two-hop security level for your DNS requests in order to protect your privacy.

This setting mainly exists for when you need to simulate your presence in another location on a lower level too. This might be necessary to defeat more intelligent geo-blocking systems.`,
		Help:            profile.SPNRulesHelp,
		Sensitive:       true,
		OptType:         config.OptTypeStringArray,
		RequiresRestart: true,
		ExpertiseLevel:  config.ExpertiseLevelExpert,
		DefaultValue:    []string{},
		Annotations: config.Annotations{
			config.CategoryAnnotation:                    "Routing",
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
		Name:            "Use Community Nodes",
		Key:             CfgOptionUseCommunityNodesKey,
		Description:     "Use nodes (servers) not operated by Safing themselves. The use of community nodes is recommended as it diversifies the ownership of the nodes you use for your connections and further strengthens your privacy. Plain connections (eg. http, smtp, ...) will never exit via community nodes, making this setting safe to use.",
		Sensitive:       true,
		OptType:         config.OptTypeBool,
		RequiresRestart: true,
		DefaultValue:    true,
		Annotations: config.Annotations{
			config.DisplayOrderAnnotation: cfgOptionUseCommunityNodesOrder,
			config.CategoryAnnotation:     "Routing",
		},
	})
	if err != nil {
		return err
	}
	cfgOptionUseCommunityNodes = config.Concurrent.GetAsBool(CfgOptionUseCommunityNodesKey, true)

	err = config.Register(&config.Option{
		Name:           "Trust Nodes",
		Key:            CfgOptionTrustNodeNodesKey,
		Description:    "Specify which community nodes to additionally trust. These nodes may then also be used as a Home Node, as well as an Exit Node for unencrypted connections.",
		Help:           "You can specify nodes by their ID or their verified operator.",
		Sensitive:      true,
		OptType:        config.OptTypeStringArray,
		ExpertiseLevel: config.ExpertiseLevelExpert,
		DefaultValue:   []string{},
		Annotations: config.Annotations{
			config.DisplayOrderAnnotation: cfgOptionTrustNodeNodesOrder,
			config.CategoryAnnotation:     "Routing",
		},
	})
	if err != nil {
		return err
	}
	cfgOptionTrustNodeNodes = config.Concurrent.GetAsStringArray(CfgOptionTrustNodeNodesKey, []string{})

	err = config.Register(&config.Option{
		Name:         "Special Access Code",
		Key:          cfgOptionSpecialAccessCodeKey,
		Description:  "Special Access Codes grant access to the SPN for testing or evaluation purposes.",
		Sensitive:    true,
		OptType:      config.OptTypeString,
		DefaultValue: cfgOptionSpecialAccessCodeDefault,
		Annotations: config.Annotations{
			config.DisplayOrderAnnotation: cfgOptionSpecialAccessCodeOrder,
			config.CategoryAnnotation:     "Advanced",
		},
	})
	if err != nil {
		return err
	}
	cfgOptionSpecialAccessCode = config.Concurrent.GetAsString(cfgOptionSpecialAccessCodeKey, "")

	if conf.PublicHub() {
		err = config.Register(&config.Option{
			Name:            "Connect From Advertised IPs Only",
			Key:             cfgOptionBindToAdvertisedKey,
			Description:     "Only connect from (bind to) the advertised IP addresses.",
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
