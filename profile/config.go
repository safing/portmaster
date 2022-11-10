package profile

import (
	"strings"

	"github.com/safing/portbase/config"
	"github.com/safing/portmaster/profile/endpoints"
	"github.com/safing/portmaster/status"
	"github.com/safing/spn/navigator"
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
	cfgOptionBlockScopeInternet      config.IntOption // security level option
	cfgOptionBlockScopeInternetOrder = 16

	CfgOptionBlockScopeLANKey   = "filter/blockLAN"
	cfgOptionBlockScopeLAN      config.IntOption // security level option
	cfgOptionBlockScopeLANOrder = 17

	CfgOptionBlockScopeLocalKey   = "filter/blockLocal"
	cfgOptionBlockScopeLocal      config.IntOption // security level option
	cfgOptionBlockScopeLocalOrder = 18

	// Connection Types.

	CfgOptionBlockP2PKey   = "filter/blockP2P"
	cfgOptionBlockP2P      config.IntOption // security level option
	cfgOptionBlockP2POrder = 19

	CfgOptionBlockInboundKey   = "filter/blockInbound"
	cfgOptionBlockInbound      config.IntOption // security level option
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
	cfgOptionFilterSubDomains      config.IntOption // security level option
	cfgOptionFilterSubDomainsOrder = 36

	// DNS Filtering.

	CfgOptionFilterCNAMEKey   = "filter/includeCNAMEs"
	cfgOptionFilterCNAME      config.IntOption // security level option
	cfgOptionFilterCNAMEOrder = 48

	CfgOptionRemoveOutOfScopeDNSKey   = "filter/removeOutOfScopeDNS"
	cfgOptionRemoveOutOfScopeDNS      config.IntOption // security level option
	cfgOptionRemoveOutOfScopeDNSOrder = 49

	CfgOptionRemoveBlockedDNSKey   = "filter/removeBlockedDNS"
	cfgOptionRemoveBlockedDNS      config.IntOption // security level option
	cfgOptionRemoveBlockedDNSOrder = 50

	CfgOptionDomainHeuristicsKey   = "filter/domainHeuristics"
	cfgOptionDomainHeuristics      config.IntOption // security level option
	cfgOptionDomainHeuristicsOrder = 51

	// Advanced.

	CfgOptionPreventBypassingKey   = "filter/preventBypassing"
	cfgOptionPreventBypassing      config.IntOption // security level option
	cfgOptionPreventBypassingOrder = 64

	CfgOptionDisableAutoPermitKey   = "filter/disableAutoPermit"
	cfgOptionDisableAutoPermit      config.IntOption // security level option
	cfgOptionDisableAutoPermitOrder = 65

	// Setting "Permanent Verdicts" at order 96.

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

	// Setting "Home Node Rules" at order 145.

	CfgOptionExitHubPolicyKey   = "spn/exitHubPolicy"
	cfgOptionExitHubPolicy      config.StringArrayOption
	cfgOptionExitHubPolicyOrder = 146

	// Setting "DNS Exit Node Rules" at order 147.
)

// A list of all security level settings.
var securityLevelSettings = []string{
	CfgOptionBlockScopeInternetKey,
	CfgOptionBlockScopeLANKey,
	CfgOptionBlockScopeLocalKey,
	CfgOptionBlockP2PKey,
	CfgOptionBlockInboundKey,
	CfgOptionFilterSubDomainsKey,
	CfgOptionFilterCNAMEKey,
	CfgOptionRemoveOutOfScopeDNSKey,
	CfgOptionRemoveBlockedDNSKey,
	CfgOptionDomainHeuristicsKey,
	CfgOptionPreventBypassingKey,
	CfgOptionDisableAutoPermitKey,
}

var (
	// SPNRulesQuickSettings is a list of countries the SPN currently is present in
	// as quick settings in order to help users with SPN related policy settings.
	// This is a quick win to make the MVP easier to use, but will be replaced by
	// a better solution in the future.
	SPNRulesQuickSettings = []config.QuickSetting{
		{Name: "Exclude Canada (CA)", Action: config.QuickMergeTop, Value: []string{"- CA"}},
		{Name: "Exclude Finland (FI)", Action: config.QuickMergeTop, Value: []string{"- FI"}},
		{Name: "Exclude France (FR)", Action: config.QuickMergeTop, Value: []string{"- FR"}},
		{Name: "Exclude Germany (DE)", Action: config.QuickMergeTop, Value: []string{"- DE"}},
		{Name: "Exclude Israel (IL)", Action: config.QuickMergeTop, Value: []string{"- IL"}},
		{Name: "Exclude Poland (PL)", Action: config.QuickMergeTop, Value: []string{"- PL"}},
		{Name: "Exclude United Kingdom (GB)", Action: config.QuickMergeTop, Value: []string{"- GB"}},
		{Name: "Exclude United States of America (US)", Action: config.QuickMergeTop, Value: []string{"- US"}},
	}

	// SPNRulesVerdictNames defines the verdicts names to be used for SPN Rules.
	SPNRulesVerdictNames = map[string]string{
		"-": "Exclude", // Default.
		"+": "Allow",
	}

	// SPNRulesHelp defines the help text for SPN related Hub selection rules.
	SPNRulesHelp = strings.ReplaceAll(`Rules are checked from top to bottom, stopping after the first match. They can match the following attributes of SPN Nodes:

- Country (based on IPs): "US"
- AS number: "AS123456"
- Address: "192.168.0.1"
- Network: "192.168.0.1/24"
- Anything: "*"
`, `"`, "`")
)

func registerConfiguration() error { //nolint:maintidx
	// Default Filter Action
	// permit - blocklist mode: everything is allowed unless blocked
	// ask - ask mode: if not verdict is found, the user is consulted
	// block - allowlist mode: everything is blocked unless explicitly allowed
	err := config.Register(&config.Option{
		Name:         "Default Network Action",
		Key:          CfgOptionDefaultActionKey,
		Description:  `The default network action is applied when nothing else allows or blocks an outgoing connection. Incoming connections are always blocked by default.`,
		OptType:      config.OptTypeString,
		DefaultValue: DefaultActionPermitValue,
		Annotations: config.Annotations{
			config.DisplayHintAnnotation:  config.DisplayHintOneOf,
			config.DisplayOrderAnnotation: cfgOptionDefaultActionOrder,
			config.CategoryAnnotation:     "General",
		},
		PossibleValues: []config.PossibleValue{
			{
				Name:        "Allow",
				Value:       DefaultActionPermitValue,
				Description: "Allow all connections",
			},
			{
				Name:        "Block",
				Value:       DefaultActionBlockValue,
				Description: "Block all connections",
			},
			{
				Name:        "Prompt",
				Value:       DefaultActionAskValue,
				Description: "Prompt for decisions",
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
		Name:         "Disable Auto Allow",
		Key:          CfgOptionDisableAutoPermitKey,
		Description:  `Auto Allow searches for a relation between an app and the destination of a connection - if there is a correlation, the connection will be allowed.`,
		OptType:      config.OptTypeInt,
		ReleaseLevel: config.ReleaseLevelBeta,
		DefaultValue: status.SecurityLevelsAll,
		Annotations: config.Annotations{
			config.DisplayOrderAnnotation: cfgOptionDisableAutoPermitOrder,
			config.DisplayHintAnnotation:  status.DisplayHintSecurityLevel,
			config.CategoryAnnotation:     "Advanced",
		},
		PossibleValues: status.AllSecurityLevelValues,
	})
	if err != nil {
		return err
	}
	cfgOptionDisableAutoPermit = config.Concurrent.GetAsInt(CfgOptionDisableAutoPermitKey, int64(status.SecurityLevelsAll))
	cfgIntOptions[CfgOptionDisableAutoPermitKey] = cfgOptionDisableAutoPermit

	rulesHelp := strings.ReplaceAll(`Rules are checked from top to bottom, stopping after the first match. They can match:

- By address: "192.168.0.1"
- By network: "192.168.0.1/24"
- By network scope: "Localhost", "LAN" or "Internet"
- By domain:
	- Matching a distinct domain: "example.com"
	- Matching a domain with subdomains: ".example.com"
	- Matching with a wildcard prefix: "*xample.com"
	- Matching with a wildcard suffix: "example.*"
	- Matching domains containing text: "*example*"
- By country (based on IP): "US"
- By AS number: "AS123456"
- By filter list - use the filterlist ID prefixed with "L:": "L:MAL"
- Match anything: "*"

Additionally, you may supply a protocol and port just behind that using numbers ("6/80") or names ("TCP/HTTP").  
Port ranges are defined by using a hyphen ("TCP/1-1024"). Omit the port to match any.  
Use a "*" for matching any protocol. If matching ports with any protocol, protocols without ports will not match.  
Rules with protocol and port definitions only match if the protocol and port also match.  
Ports are always compared to the destination port, thus, the local listening port for incoming connections.  
Examples: "192.168.0.1 TCP/HTTP", "LAN UDP/50000-55000", "example.com */HTTPS", "1.1.1.1 ICMP"

Important: DNS Requests are only matched against domain and filter list rules, all others require an IP address and are checked only with the following IP connection.
`, `"`, "`")

	// rulesVerdictNames defines the verdicts names to be used for filter rules.
	rulesVerdictNames := map[string]string{
		"-": "Block", // Default.
		"+": "Allow",
	}

	// Endpoint Filter List
	err = config.Register(&config.Option{
		Name:         "Outgoing Rules",
		Key:          CfgOptionEndpointsKey,
		Description:  "Rules that apply to outgoing network connections. Cannot overrule Network Scopes and Connection Types (see above).",
		Help:         rulesHelp,
		Sensitive:    true,
		OptType:      config.OptTypeStringArray,
		DefaultValue: []string{},
		Annotations: config.Annotations{
			config.StackableAnnotation:                   true,
			config.DisplayHintAnnotation:                 endpoints.DisplayHintEndpointList,
			config.DisplayOrderAnnotation:                cfgOptionEndpointsOrder,
			config.CategoryAnnotation:                    "Rules",
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
		Name:           "Incoming Rules",
		Key:            CfgOptionServiceEndpointsKey,
		Description:    "Rules that apply to incoming network connections. Cannot overrule Network Scopes and Connection Types (see above).",
		Help:           rulesHelp,
		Sensitive:      true,
		OptType:        config.OptTypeStringArray,
		DefaultValue:   []string{},
		ExpertiseLevel: config.ExpertiseLevelExpert,
		Annotations: config.Annotations{
			config.StackableAnnotation:                   true,
			config.DisplayHintAnnotation:                 endpoints.DisplayHintEndpointList,
			config.DisplayOrderAnnotation:                cfgOptionServiceEndpointsOrder,
			config.CategoryAnnotation:                    "Rules",
			endpoints.EndpointListVerdictNamesAnnotation: rulesVerdictNames,
			config.QuickSettingsAnnotation: []config.QuickSetting{
				{
					Name:   "Allow SSH",
					Action: config.QuickMergeTop,
					Value:  []string{"+ * tcp/22"},
				},
				{
					Name:   "Allow HTTP/s",
					Action: config.QuickMergeTop,
					Value:  []string{"+ * tcp/80", "+ * tcp/443"},
				},
				{
					Name:   "Allow RDP",
					Action: config.QuickMergeTop,
					Value:  []string{"+ * */3389"},
				},
				{
					Name:   "Allow all from LAN",
					Action: config.QuickMergeTop,
					Value:  []string{"+ LAN"},
				},
				{
					Name:   "Allow all from Internet",
					Action: config.QuickMergeTop,
					Value:  []string{"+ Internet"},
				},
				{
					Name:   "Block everything else",
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
		Name:         "Filter Lists",
		Key:          CfgOptionFilterListsKey,
		Description:  "Block connections that match enabled filter lists.",
		OptType:      config.OptTypeStringArray,
		DefaultValue: defaultFilterListsValue,
		Annotations: config.Annotations{
			config.DisplayHintAnnotation:  "filter list",
			config.DisplayOrderAnnotation: cfgOptionFilterListsOrder,
			config.CategoryAnnotation:     "Filter Lists",
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
		Name:           "Block Domain Aliases",
		Key:            CfgOptionFilterCNAMEKey,
		Description:    "Block a domain if a resolved CNAME (alias) is blocked by a rule or filter list.",
		OptType:        config.OptTypeInt,
		DefaultValue:   status.SecurityLevelsAll,
		ExpertiseLevel: config.ExpertiseLevelExpert,
		Annotations: config.Annotations{
			config.DisplayHintAnnotation:  status.DisplayHintSecurityLevel,
			config.DisplayOrderAnnotation: cfgOptionFilterCNAMEOrder,
			config.CategoryAnnotation:     "DNS Filtering",
		},
		PossibleValues: status.AllSecurityLevelValues,
	})
	if err != nil {
		return err
	}
	cfgOptionFilterCNAME = config.Concurrent.GetAsInt(CfgOptionFilterCNAMEKey, int64(status.SecurityLevelsAll))
	cfgIntOptions[CfgOptionFilterCNAMEKey] = cfgOptionFilterCNAME

	// Include subdomains
	err = config.Register(&config.Option{
		Name:           "Block Subdomains of Filter List Entries",
		Key:            CfgOptionFilterSubDomainsKey,
		Description:    "Additionally block all subdomains of entries in selected filter lists.",
		OptType:        config.OptTypeInt,
		DefaultValue:   status.SecurityLevelsAll,
		PossibleValues: status.AllSecurityLevelValues,
		Annotations: config.Annotations{
			config.DisplayHintAnnotation:  status.DisplayHintSecurityLevel,
			config.DisplayOrderAnnotation: cfgOptionFilterSubDomainsOrder,
			config.CategoryAnnotation:     "Filter Lists",
		},
	})
	if err != nil {
		return err
	}
	cfgOptionFilterSubDomains = config.Concurrent.GetAsInt(CfgOptionFilterSubDomainsKey, int64(status.SecurityLevelsAll))
	cfgIntOptions[CfgOptionFilterSubDomainsKey] = cfgOptionFilterSubDomains

	// Block Scope Local
	err = config.Register(&config.Option{
		Name:           "Force Block Device-Local Connections",
		Key:            CfgOptionBlockScopeLocalKey,
		Description:    "Force Block all internal connections on your own device, ie. localhost. Is stronger than Rules (see below).",
		OptType:        config.OptTypeInt,
		ExpertiseLevel: config.ExpertiseLevelExpert,
		DefaultValue:   status.SecurityLevelOff,
		PossibleValues: status.AllSecurityLevelValues,
		Annotations: config.Annotations{
			config.DisplayHintAnnotation:  status.DisplayHintSecurityLevel,
			config.DisplayOrderAnnotation: cfgOptionBlockScopeLocalOrder,
			config.CategoryAnnotation:     "Network Scope",
		},
	})
	if err != nil {
		return err
	}
	cfgOptionBlockScopeLocal = config.Concurrent.GetAsInt(CfgOptionBlockScopeLocalKey, int64(status.SecurityLevelOff))
	cfgIntOptions[CfgOptionBlockScopeLocalKey] = cfgOptionBlockScopeLocal

	// Block Scope LAN
	err = config.Register(&config.Option{
		Name:           "Force Block LAN",
		Key:            CfgOptionBlockScopeLANKey,
		Description:    "Force Block all connections from and to the Local Area Network. Is stronger than Rules (see below).",
		OptType:        config.OptTypeInt,
		DefaultValue:   status.SecurityLevelOff,
		PossibleValues: status.AllSecurityLevelValues,
		Annotations: config.Annotations{
			config.DisplayHintAnnotation:  status.DisplayHintSecurityLevel,
			config.DisplayOrderAnnotation: cfgOptionBlockScopeLANOrder,
			config.CategoryAnnotation:     "Network Scope",
		},
	})
	if err != nil {
		return err
	}
	cfgOptionBlockScopeLAN = config.Concurrent.GetAsInt(CfgOptionBlockScopeLANKey, int64(status.SecurityLevelOff))
	cfgIntOptions[CfgOptionBlockScopeLANKey] = cfgOptionBlockScopeLAN

	// Block Scope Internet
	err = config.Register(&config.Option{
		Name:           "Force Block Internet Access",
		Key:            CfgOptionBlockScopeInternetKey,
		Description:    "Force Block connections from and to the Internet. Is stronger than Rules (see below).",
		OptType:        config.OptTypeInt,
		DefaultValue:   status.SecurityLevelOff,
		PossibleValues: status.AllSecurityLevelValues,
		Annotations: config.Annotations{
			config.DisplayHintAnnotation:  status.DisplayHintSecurityLevel,
			config.DisplayOrderAnnotation: cfgOptionBlockScopeInternetOrder,
			config.CategoryAnnotation:     "Network Scope",
		},
	})
	if err != nil {
		return err
	}
	cfgOptionBlockScopeInternet = config.Concurrent.GetAsInt(CfgOptionBlockScopeInternetKey, int64(status.SecurityLevelOff))
	cfgIntOptions[CfgOptionBlockScopeInternetKey] = cfgOptionBlockScopeInternet

	// Block Peer to Peer Connections
	err = config.Register(&config.Option{
		Name:           "Force Block P2P/Direct Connections",
		Key:            CfgOptionBlockP2PKey,
		Description:    "These are connections that are established directly to an IP address or peer on the Internet without resolving a domain name via DNS first. Is stronger than Rules (see below).",
		OptType:        config.OptTypeInt,
		DefaultValue:   status.SecurityLevelOff,
		PossibleValues: status.AllSecurityLevelValues,
		Annotations: config.Annotations{
			config.DisplayHintAnnotation:  status.DisplayHintSecurityLevel,
			config.DisplayOrderAnnotation: cfgOptionBlockP2POrder,
			config.CategoryAnnotation:     "Connection Types",
		},
	})
	if err != nil {
		return err
	}
	cfgOptionBlockP2P = config.Concurrent.GetAsInt(CfgOptionBlockP2PKey, int64(status.SecurityLevelOff))
	cfgIntOptions[CfgOptionBlockP2PKey] = cfgOptionBlockP2P

	// Block Inbound Connections
	err = config.Register(&config.Option{
		Name:           "Force Block Incoming Connections",
		Key:            CfgOptionBlockInboundKey,
		Description:    "Connections initiated towards your device from the LAN or Internet. This will usually only be the case if you are running a network service or are using peer to peer software. Is stronger than Rules (see below).",
		OptType:        config.OptTypeInt,
		DefaultValue:   status.SecurityLevelsAll,
		PossibleValues: status.AllSecurityLevelValues,
		Annotations: config.Annotations{
			config.DisplayHintAnnotation:  status.DisplayHintSecurityLevel,
			config.DisplayOrderAnnotation: cfgOptionBlockInboundOrder,
			config.CategoryAnnotation:     "Connection Types",
		},
	})
	if err != nil {
		return err
	}
	cfgOptionBlockInbound = config.Concurrent.GetAsInt(CfgOptionBlockInboundKey, int64(status.SecurityLevelOff))
	cfgIntOptions[CfgOptionBlockInboundKey] = cfgOptionBlockInbound

	// Filter Out-of-Scope DNS Records
	err = config.Register(&config.Option{
		Name:           "Enforce Global/Private Split-View",
		Key:            CfgOptionRemoveOutOfScopeDNSKey,
		Description:    "Reject private IP addresses (RFC1918 et al.) from public DNS responses. If the system resolver is in use, the resulting connection will be blocked instead of the DNS request.",
		OptType:        config.OptTypeInt,
		ExpertiseLevel: config.ExpertiseLevelDeveloper,
		DefaultValue:   status.SecurityLevelsAll,
		PossibleValues: status.AllSecurityLevelValues,
		Annotations: config.Annotations{
			config.DisplayHintAnnotation:  status.DisplayHintSecurityLevel,
			config.DisplayOrderAnnotation: cfgOptionRemoveOutOfScopeDNSOrder,
			config.CategoryAnnotation:     "DNS Filtering",
		},
	})
	if err != nil {
		return err
	}
	cfgOptionRemoveOutOfScopeDNS = config.Concurrent.GetAsInt(CfgOptionRemoveOutOfScopeDNSKey, int64(status.SecurityLevelsAll))
	cfgIntOptions[CfgOptionRemoveOutOfScopeDNSKey] = cfgOptionRemoveOutOfScopeDNS

	// Filter DNS Records that would be blocked
	err = config.Register(&config.Option{
		Name:           "Reject Blocked IPs",
		Key:            CfgOptionRemoveBlockedDNSKey,
		Description:    "Reject blocked IP addresses directly from the DNS response instead of handing them over to the app and blocking a resulting connection. This settings does not affect privacy and only takes effect when the system resolver is not in use.",
		OptType:        config.OptTypeInt,
		ExpertiseLevel: config.ExpertiseLevelDeveloper,
		DefaultValue:   status.SecurityLevelsAll,
		PossibleValues: status.AllSecurityLevelValues,
		Annotations: config.Annotations{
			config.DisplayHintAnnotation:  status.DisplayHintSecurityLevel,
			config.DisplayOrderAnnotation: cfgOptionRemoveBlockedDNSOrder,
			config.CategoryAnnotation:     "DNS Filtering",
		},
	})
	if err != nil {
		return err
	}
	cfgOptionRemoveBlockedDNS = config.Concurrent.GetAsInt(CfgOptionRemoveBlockedDNSKey, int64(status.SecurityLevelsAll))
	cfgIntOptions[CfgOptionRemoveBlockedDNSKey] = cfgOptionRemoveBlockedDNS

	// Domain heuristics
	err = config.Register(&config.Option{
		Name:           "Enable Domain Heuristics",
		Key:            CfgOptionDomainHeuristicsKey,
		Description:    "Checks for suspicious domain names and blocks them. This option currently targets domain names generated by malware and DNS data exfiltration channels.",
		OptType:        config.OptTypeInt,
		ExpertiseLevel: config.ExpertiseLevelExpert,
		DefaultValue:   status.SecurityLevelsAll,
		PossibleValues: status.AllSecurityLevelValues,
		Annotations: config.Annotations{
			config.DisplayHintAnnotation:  status.DisplayHintSecurityLevel,
			config.DisplayOrderAnnotation: cfgOptionDomainHeuristicsOrder,
			config.CategoryAnnotation:     "DNS Filtering",
		},
	})
	if err != nil {
		return err
	}
	cfgOptionDomainHeuristics = config.Concurrent.GetAsInt(CfgOptionDomainHeuristicsKey, int64(status.SecurityLevelsAll))
	cfgIntOptions[CfgOptionDomainHeuristicsKey] = cfgOptionDomainHeuristics

	// Bypass prevention
	err = config.Register(&config.Option{
		Name: "Block Bypassing",
		Key:  CfgOptionPreventBypassingKey,
		Description: `Prevent apps from bypassing Portmaster's privacy protections.  
If Block Bypassing is disabled, Portmaster can no longer protect you or filter connections from the affected applications.

Current Features:  
- Disable Firefox' internal DNS-over-HTTPs resolver
- Block direct access to public DNS resolvers

Please note that if you are using the system resolver, bypass attempts might be additionally blocked there too.`,
		OptType:        config.OptTypeInt,
		ExpertiseLevel: config.ExpertiseLevelUser,
		ReleaseLevel:   config.ReleaseLevelStable,
		DefaultValue:   status.SecurityLevelsAll,
		PossibleValues: status.AllSecurityLevelValues,
		Annotations: config.Annotations{
			config.DisplayHintAnnotation:  status.DisplayHintSecurityLevel,
			config.DisplayOrderAnnotation: cfgOptionPreventBypassingOrder,
			config.CategoryAnnotation:     "Advanced",
		},
	})
	if err != nil {
		return err
	}
	cfgOptionPreventBypassing = config.Concurrent.GetAsInt((CfgOptionPreventBypassingKey), int64(status.SecurityLevelsAll))
	cfgIntOptions[CfgOptionPreventBypassingKey] = cfgOptionPreventBypassing

	// Use SPN
	err = config.Register(&config.Option{
		Name:         "Use SPN",
		Key:          CfgOptionUseSPNKey,
		Description:  "Protect network traffic with the Safing Privacy Network. If the SPN is not available or the connection is interrupted, network traffic will be blocked.",
		OptType:      config.OptTypeBool,
		DefaultValue: true,
		Annotations: config.Annotations{
			config.DisplayOrderAnnotation: cfgOptionUseSPNOrder,
			config.CategoryAnnotation:     "General",
		},
	})
	if err != nil {
		return err
	}
	cfgOptionUseSPN = config.Concurrent.GetAsBool(CfgOptionUseSPNKey, true)
	cfgBoolOptions[CfgOptionUseSPNKey] = cfgOptionUseSPN

	// SPN Rules
	err = config.Register(&config.Option{
		Name:         "SPN Rules",
		Key:          CfgOptionSPNUsagePolicyKey,
		Description:  `Customize which websites should or should not be routed through the SPN. Only active if "Use SPN" is enabled.`,
		Help:         rulesHelp,
		Sensitive:    true,
		OptType:      config.OptTypeStringArray,
		DefaultValue: []string{},
		Annotations: config.Annotations{
			config.StackableAnnotation:                   true,
			config.CategoryAnnotation:                    "General",
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

	// Exit Node Rules
	err = config.Register(&config.Option{
		Name: "Exit Node Rules",
		Key:  CfgOptionExitHubPolicyKey,
		Description: `Customize which countries should or should not be used for your Exit Nodes. Exit Nodes are used to exit the SPN and establish a connection to your destination.

By default, the Portmaster tries to choose the node closest to the destination as the Exit Node. This reduces your exposure to the open Internet. Exit Nodes are chosen for every destination separately.`,
		Help:         SPNRulesHelp,
		Sensitive:    true,
		OptType:      config.OptTypeStringArray,
		DefaultValue: []string{},
		Annotations: config.Annotations{
			config.StackableAnnotation:                   true,
			config.CategoryAnnotation:                    "Routing",
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
		Name:         "Select SPN Routing Algorithm",
		Key:          CfgOptionRoutingAlgorithmKey,
		Description:  "Select the routing algorithm for your connections through the SPN. Configure your preferred balance between speed and privacy. Portmaster may automatically upgrade the routing algorithm if necessary to protect your privacy.",
		OptType:      config.OptTypeString,
		DefaultValue: navigator.DefaultRoutingProfileID,
		Annotations: config.Annotations{
			config.DisplayHintAnnotation:  config.DisplayHintOneOf,
			config.DisplayOrderAnnotation: cfgOptionRoutingAlgorithmOrder,
			config.CategoryAnnotation:     "Routing",
		},
		PossibleValues: []config.PossibleValue{
			{
				Name:        "Plain VPN Mode",
				Value:       "home",
				Description: "Always connect to the destination directly from the Home Hub. Only provides very basic privacy, as the Home Hub both knows where you are coming from and where you are connecting to.",
			},
			{
				Name:        "Speed Focused",
				Value:       "single-hop",
				Description: "Optimize routes with a minimum of one hop. Provides good speeds. This will often use the Home Hub to connect to destinations near you, but will use more hops to far away destinations for better privacy over long distances.",
			},
			{
				Name:        "Balanced",
				Value:       "double-hop",
				Description: "Optimize routes with a minimum of two hops. Provides good privacy as well as good speeds. No single node knows where you are coming from *and* where you are connecting to.",
			},
			{
				Name:        "Privacy Focused",
				Value:       "triple-hop",
				Description: "Optimize routes with a minimum of three hops. Provides very good privacy. No single node knows where you are coming from *and* where you are connecting to - with an additional hop just to be sure.",
			},
		},
	})
	if err != nil {
		return err
	}
	cfgOptionRoutingAlgorithm = config.Concurrent.GetAsString(CfgOptionRoutingAlgorithmKey, navigator.DefaultRoutingProfileID)
	cfgStringOptions[CfgOptionRoutingAlgorithmKey] = cfgOptionRoutingAlgorithm

	return nil
}
